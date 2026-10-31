package ops

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestPreflightReportsAllProblems(t *testing.T) {
	originalEUID, originalStatfs, originalReadFile := preflightEUID, preflightStatfs, preflightReadFile
	t.Cleanup(func() {
		preflightEUID, preflightStatfs, preflightReadFile = originalEUID, originalStatfs, originalReadFile
	})
	preflightEUID = func() int { return 1000 }
	preflightStatfs = func(string) (uint64, error) { return 1, nil }
	preflightReadFile = func(string) ([]byte, error) { return []byte("ports: 127.0.0.1:${ZOE_SERVER_PORT:-8787}:8787\n"), nil }
	manifest := ReleaseManifest{Architecture: "arm64"}
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "ss":
			return []byte("LISTEN 0 128 127.0.0.1:8787 0.0.0.0:*\n"), nil
		default:
			return nil, errors.New("missing")
		}
	}
	problems := NewManager(run).preflight(context.Background(), LifecycleOptions{OptRoot: "/opt/zoe", DataRoot: "/var/lib/zoe", Release: "/release"}, manifest)
	if len(problems) < 6 {
		t.Fatalf("got %d problems, want all host failures", len(problems))
	}
	message := formatPreflightProblems(problems).Error()
	for _, want := range []string{"must run as root", "Docker is required", "docker compose plugin", "Docker is not running", "insufficient free space", "required port 8787"} {
		if !strings.Contains(message, want) {
			t.Errorf("preflight error missing %q: %s", want, message)
		}
	}
}

func TestPreflightPassesWithInjectableHost(t *testing.T) {
	originalEUID, originalStatfs, originalReadFile := preflightEUID, preflightStatfs, preflightReadFile
	t.Cleanup(func() {
		preflightEUID, preflightStatfs, preflightReadFile = originalEUID, originalStatfs, originalReadFile
	})
	preflightEUID = func() int { return 0 }
	preflightStatfs = func(string) (uint64, error) { return MinimumFreeBytes, nil }
	preflightReadFile = func(string) ([]byte, error) { return []byte("127.0.0.1:${ZOE_SERVER_PORT:-8787}:8787\n"), nil }
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "ss" {
			return []byte("LISTEN 0 128 127.0.0.1:9999 0.0.0.0:*\n"), nil
		}
		return []byte("ok"), nil
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("host-specific preflight")
	}
	if got := NewManager(run).preflight(context.Background(), LifecycleOptions{OptRoot: "/opt/zoe", DataRoot: "/var/lib/zoe", Release: "/release"}, ReleaseManifest{Architecture: runtime.GOARCH}); len(got) != 0 {
		t.Fatalf("unexpected preflight problems: %v", got)
	}
}
