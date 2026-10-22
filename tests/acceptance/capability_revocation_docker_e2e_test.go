package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

// This opt-in test exercises revocation through the real HTTP control plane
// and independent cylo-runner process against a local Docker daemon. The
// runner uses its configured shared host authority DB; each run has a fresh
// deployment identity and leaves that DB intact.
func TestDockerBackedCapabilityRevocationLifecycle(t *testing.T) {
	if os.Getenv("CYLO_TEST_DOCKER_CAPABILITY_REVOCATION") != "1" {
		t.Skip("set CYLO_TEST_DOCKER_CAPABILITY_REVOCATION=1 to run live Docker revocation acceptance")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI is unavailable")
	}
	if output, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("Docker daemon is unavailable: %s", strings.TrimSpace(string(output)))
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	image := capabilityTestImage(t, ctx)
	root := t.TempDir()
	databasePath := filepath.Join(root, "control.sqlite")
	journalPath := filepath.Join(root, "runner-journal.sqlite")
	bootstrapCode, deviceToken, runnerToken := reviewToken(t), reviewToken(t), reviewToken(t)
	runnerBinary := buildCapabilityRunner(t, root)
	faults := &capabilityHTTPFaults{}
	store, httpServer := startCapabilityHTTPFixture(t, databasePath, bootstrapCode, runnerToken, faults)
	defer func() {
		httpServer.Close()
		_ = store.Close()
	}()
	capabilityHTTP(t, httpServer.Client(), http.MethodPost, httpServer.URL+"/v1/bootstrap", "", "", map[string]string{
		"request_id": "docker-cap-revoke-bootstrap", "code": bootstrapCode, "device_name": "local-docker-revoke", "device_token": deviceToken,
	}, http.StatusCreated, nil)

	manifest := capabilityServiceManifest(image, "--directory", "/tmp")
	manifest.Mounts = []contracts.CapabilityMount{{VolumeID: "revocation-persistent-data", Target: "/data", ReadOnly: true}}
	release := submitCapabilityRelease(t, httpServer.Client(), httpServer.URL, deviceToken, manifest, "revoke-release")
	deployment, version := requestCapabilityDeployment(t, httpServer.Client(), httpServer.URL, deviceToken, "local-revocable-service", release, "install", nil, "revoke-install-request")
	containerName := "cylo-cap-" + capabilityShortHash(deployment)
	volumeName := "cylo-vol-" + capabilityShortHash("default\x00"+manifest.Mounts[0].VolumeID)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "--force", containerName).Run()
		_ = exec.Command("docker", "volume", "rm", volumeName).Run()
	})
	version = authorizeCapabilityDeployment(t, httpServer.Client(), httpServer.URL, deviceToken, deployment, version, "install", manifest.Budget, "revoke-install-authorize")
	if err := runCapabilityRunnerOnce(t, runnerBinary, httpServer.URL, runnerToken, journalPath); err != nil {
		t.Fatalf("runner process failed to install synthetic service: %v", err)
	}
	version = assertCapabilityDeploymentState(t, httpServer.Client(), httpServer.URL, deviceToken, deployment, "running", version)
	containerID := capabilityDockerOutput(t, ctx, "inspect", "--format", "{{.Id}}", containerName)
	if got := capabilityDockerOutput(t, ctx, "inspect", "--format", "{{.State.Running}}", containerName); got != "true" {
		t.Fatalf("synthetic service was not running before revocation: %q", got)
	}
	volumeID := capabilityDockerOutput(t, ctx, "volume", "inspect", "--format", "{{.Name}}", volumeName)
	if volumeID != volumeName {
		t.Fatalf("persistent volume name=%q, want %q", volumeID, volumeName)
	}

	var revokeReceipt contracts.CommandReceipt
	capabilityHTTP(t, httpServer.Client(), http.MethodPost, httpServer.URL+"/v1/commands", deviceToken, "revoke-running-service", contracts.CommandRequest{
		Kind: "capability.deployment.revoke", ExpectedVersion: &version, Payload: jsonRaw(t, map[string]string{"deployment_id": deployment}),
	}, http.StatusOK, &revokeReceipt)
	version = revokeReceipt.ResourceVersion
	var revokedState struct {
		DesiredState  string  `json:"desired_state"`
		ObservedState string  `json:"observed_state"`
		CleanupState  *string `json:"revocation_cleanup_state"`
		RevokedAt     *string `json:"revoked_at"`
	}
	capabilityHTTP(t, httpServer.Client(), http.MethodGet, httpServer.URL+"/v1/capabilities/deployments/"+deployment, deviceToken, "", nil, http.StatusOK, &revokedState)
	if revokedState.DesiredState != "stopped" || revokedState.ObservedState != "running" || revokedState.CleanupState == nil || *revokedState.CleanupState != "pending" || revokedState.RevokedAt == nil {
		t.Fatalf("revocation did not preserve observed state and expose pending cleanup: %+v", revokedState)
	}
	blocked := contracts.CommandRequest{Kind: "capability.deployment.request", ExpectedVersion: &version, Payload: jsonRaw(t, map[string]string{"service_id": "local-revocable-service", "release_digest": release, "operation": "install"})}
	capabilityHTTP(t, httpServer.Client(), http.MethodPost, httpServer.URL+"/v1/commands", deviceToken, "revoke-blocked-request", blocked, http.StatusConflict, nil)

	// Simulate the runner losing its HTTP receipt after it has stopped Docker.
	// A new runner process must replay its journaled observation and receive the
	// control-plane ACK without dispatching another Docker operation.
	faults.failNextDeploymentObservation.Store(true)
	if err := runCapabilityRunnerOnce(t, runnerBinary, httpServer.URL, runnerToken, journalPath); err == nil {
		t.Fatal("runner unexpectedly received the deliberately interrupted cleanup receipt")
	}
	if got := capabilityDockerOutput(t, ctx, "inspect", "--format", "{{.State.Running}}", containerName); got != "false" {
		t.Fatalf("revocation did not stop the exact synthetic container: running=%q id=%s", got, containerID)
	}
	if got := capabilityDockerOutput(t, ctx, "inspect", "--format", "{{.Id}}", containerName); got != containerID {
		t.Fatalf("revocation replaced the synthetic container: before=%s after=%s", containerID, got)
	}
	if err := runCapabilityRunnerOnce(t, runnerBinary, httpServer.URL, runnerToken, journalPath); err != nil {
		t.Fatalf("restarted runner could not replay cleanup receipt: %v", err)
	}
	if state, acknowledged := capabilityJournalReceipt(t, journalPath, deployment); state != "stopped" || !acknowledged {
		t.Fatalf("runner restart did not acknowledge durable cleanup receipt: state=%q acknowledged=%v", state, acknowledged)
	}
	var confirmedState struct {
		ObservedState string  `json:"observed_state"`
		CleanupState  *string `json:"revocation_cleanup_state"`
	}
	capabilityHTTP(t, httpServer.Client(), http.MethodGet, httpServer.URL+"/v1/capabilities/deployments/"+deployment, deviceToken, "", nil, http.StatusOK, &confirmedState)
	if confirmedState.ObservedState != "stopped" || confirmedState.CleanupState == nil || *confirmedState.CleanupState != "confirmed" {
		t.Fatalf("replayed runner receipt did not confirm cleanup: %+v", confirmedState)
	}
	if got := capabilityDockerOutput(t, ctx, "volume", "inspect", "--format", "{{.Name}}", volumeName); got != volumeID {
		t.Fatalf("revocation removed or replaced persistent volume: before=%s after=%s", volumeID, got)
	}
	_, nextVersion := requestCapabilityDeployment(t, httpServer.Client(), httpServer.URL, deviceToken, "local-revocable-service", release, "install", &version, "revoke-after-confirmation")
	_ = nextVersion
	var cleanupBody []byte
	if err := store.DB().QueryRowContext(ctx, `SELECT body FROM capability_observations WHERE deployment_id=? ORDER BY observed_at DESC LIMIT 1`, deployment).Scan(&cleanupBody); err != nil {
		t.Fatal(err)
	}
	var cleanupObservation contracts.DeploymentObservation
	if err := json.Unmarshal(cleanupBody, &cleanupObservation); err != nil {
		t.Fatal(err)
	}
	capabilityHTTP(t, httpServer.Client(), http.MethodPost, httpServer.URL+"/runner/v1/deployments/"+deployment+"/observations", runnerToken, "", cleanupObservation, http.StatusAccepted, nil)
}

func capabilityDockerOutput(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}
