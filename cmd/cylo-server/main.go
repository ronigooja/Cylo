// cylo-server starts the private control plane. Runtime profiles and bootstrap
// credentials are provided by the deployment environment; no credentials are
// read from source-controlled files or printed by the process.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ronigooja/Cylo/internal/capabilities"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/memory"
	"github.com/ronigooja/Cylo/internal/storage"
	"github.com/ronigooja/Cylo/internal/voice"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "recover-bootstrap" {
		if err := recoverBootstrap(os.Args[2:]); err != nil {
			log.Printf("bootstrap recovery failed: %v", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		log.Printf("cylo-server stopped: %v", err)
		os.Exit(1)
	}
}

func recoverBootstrap(args []string) error {
	flags := flag.NewFlagSet("recover-bootstrap", flag.ContinueOnError)
	dbPath := flags.String("database", "", "path to the control database")
	confirmed := flags.Bool("confirm-revoke-all-devices", false, "revoke all device credentials and request active runs to stop")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || !*confirmed || flags.NArg() != 0 {
		return errors.New("usage: cylo-server recover-bootstrap --database PATH --confirm-revoke-all-devices")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := storage.Open(ctx, storage.Config{Path: *dbPath})
	if err != nil {
		return err
	}
	defer store.Close()
	if err = storage.RegisterCoreMigrations(store); err != nil {
		return err
	}
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		return err
	}
	if err = memory.RegisterMigrations(store); err != nil {
		return err
	}
	if err = voice.RegisterMigrations(store); err != nil {
		return err
	}
	if err = capabilities.RegisterMigrations(store); err != nil {
		return err
	}
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return err
	}
	code := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(code))
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE devices SET status='revoked',version=version+1,revoked_at=? WHERE status!='revoked'`, now)
	if err != nil {
		return err
	}
	revoked, _ := result.RowsAffected()
	if _, err = tx.ExecContext(ctx, `UPDATE device_push_tokens SET status='disabled',disabled_at=?,updated_at=? WHERE status='active'`, now, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE push_deliveries SET status='disabled',updated_at=? WHERE status='pending'`, now); err != nil {
		return err
	}
	var stopCount int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE status IN ('dispatching','active','uncertain') AND desired_control!='stop'`).Scan(&stopCount); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE runs SET desired_control='stop',control_version=control_version+1 WHERE status IN ('dispatching','active','uncertain') AND desired_control!='stop'`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bootstrap_state(id,code_hash) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET code_hash=excluded.code_hash,consumed_by_device_id=NULL,consumed_at=NULL`, hash[:]); err != nil {
		return err
	}
	eventID, err := recoveryEventID()
	if err != nil {
		return err
	}
	var cursor int64
	if err = tx.QueryRowContext(ctx, `UPDATE event_counters SET current_cursor=current_cursor+1 WHERE workspace_id='default' RETURNING current_cursor`).Scan(&cursor); err != nil {
		return err
	}
	event := map[string]any{"event_id": eventID, "cursor": fmt.Sprint(cursor), "schema_version": 1, "type": "admin.bootstrap_recovered", "occurred_at": now, "workspace_id": "default", "aggregate_type": "admin", "aggregate_id": "bootstrap", "aggregate_version": cursor, "payload": map[string]any{"revoked_device_count": revoked, "stop_requested_run_count": stopCount}}
	payload, _ := json.Marshal(event)
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,aggregate_type,aggregate_id,aggregate_version,payload) VALUES(?,?,?,?,?,?,?,?,?,?)`, eventID, "default", cursor, 1, "admin.bootstrap_recovered", now, "admin", "bootstrap", cursor, payload); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	fmt.Println("Bootstrap recovery code (store it in deployment-managed secret configuration; shown once):")
	fmt.Println(code)
	return nil
}

func recoveryEventID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func run() error {
	addr := env("CYLO_LISTEN_ADDR", "127.0.0.1:8787")
	dbPath := strings.TrimSpace(os.Getenv("CYLO_DATABASE_PATH"))
	if dbPath == "" {
		return errors.New("CYLO_DATABASE_PATH is required")
	}
	profiles, err := loadProfiles(os.Getenv("CYLO_RUNTIME_PROFILES"))
	if err != nil {
		return err
	}
	memoryRunner, err := loadMemoryRunnerCredentialFromEnv()
	if err != nil {
		return err
	}
	bootstrapCode := os.Getenv("CYLO_BOOTSTRAP_CODE")
	runnerToken := os.Getenv("CYLO_RUNNER_TOKEN")
	if !validBearer(bootstrapCode) {
		return errors.New("CYLO_BOOTSTRAP_CODE must be a 32-byte base64url credential")
	}
	if runnerToken != "" && !validBearer(runnerToken) {
		return errors.New("CYLO_RUNNER_TOKEN must be a 32-byte base64url bearer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := storage.Open(ctx, storage.Config{Path: dbPath})
	if err != nil {
		return err
	}
	defer store.Close()
	if err = storage.RegisterCoreMigrations(store); err != nil {
		return err
	}
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		return err
	}
	pushSender, err := configuredPushSender()
	if err != nil {
		return err
	}
	cfg := control.Config{
		BootstrapCode:      bootstrapCode,
		RunnerToken:        runnerToken,
		RunnerID:           env("CYLO_RUNNER_ID", "configured-runner"),
		RunnerWorkspaceIDs: splitEnv("CYLO_RUNNER_WORKSPACES", "default"),
		RunnerScopes:       splitEnv("CYLO_RUNNER_SCOPES", "runner:claim,runner:observe,runner:control:read"),
		DeviceScopes:       optionalSplitEnv("CYLO_OWNER_SCOPES"),
		DefaultProfileID:   strings.TrimSpace(os.Getenv("CYLO_DEFAULT_PROFILE_ID")),
		Profiles:           profiles,
		PushSender:         pushSender,
	}
	server, err := composeControlServer(store, cfg, voice.Config{GatewayURL: os.Getenv("CYLO_VOICE_GATEWAY_URL"), GatewayAPIKey: os.Getenv("CYLO_VOICE_GATEWAY_API_KEY")}, capabilities.Config{
		AvailableCapabilityIDs: splitEnv("CYLO_AVAILABLE_CAPABILITY_IDS", ""),
		RuntimeBackend:         strings.TrimSpace(os.Getenv("CYLO_CAPABILITY_RUNTIME_BACKEND")),
	}, memory.DerivedConfig{})
	if err != nil {
		return err
	}
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	if runnerToken != "" {
		if err = control.RegisterRunnerCredentialIfMissing(ctx, store, runnerToken, env("CYLO_RUNNER_ID", "configured-runner"), splitEnv("CYLO_RUNNER_WORKSPACES", "default"), splitEnv("CYLO_RUNNER_SCOPES", "runner:claim,runner:observe,runner:control:read")); err != nil {
			return err
		}
	}
	if memoryRunner.Enabled {
		if err = registerMemoryRunnerCredential(ctx, store, memoryRunner); err != nil {
			return err
		}
	}
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE streams are bounded by request context and shutdown.
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("cylo-server listening on %s", listener.Addr())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()
	pushStop := make(chan struct{})
	if pushSender != nil {
		go runPushWorker(pushStop, server)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case err = <-serveErr:
		close(pushStop)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signals:
		close(pushStop)
	}
	shutdown, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	if err = httpServer.Shutdown(shutdown); err != nil {
		_ = httpServer.Close()
		return err
	}
	if err = <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func configuredPushSender() (control.PushSender, error) {
	envName := strings.TrimSpace(os.Getenv("CYLO_APNS_ENVIRONMENT"))
	teamID := strings.TrimSpace(os.Getenv("CYLO_APNS_TEAM_ID"))
	keyID := strings.TrimSpace(os.Getenv("CYLO_APNS_KEY_ID"))
	topic := strings.TrimSpace(os.Getenv("CYLO_APNS_TOPIC"))
	keyPath := strings.TrimSpace(os.Getenv("CYLO_APNS_PRIVATE_KEY_PATH"))
	configured := 0
	for _, value := range []string{envName, teamID, keyID, topic, keyPath} {
		if value != "" {
			configured++
		}
	}
	if configured == 0 {
		return nil, nil
	}
	if configured != 5 {
		return nil, errors.New("APNs configuration requires environment, team ID, key ID, topic, and private key path")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, errors.New("could not read deployment-provided APNs private key")
	}
	sender, err := control.NewAPNSSender(control.APNSConfig{Environment: envName, TeamID: teamID, KeyID: keyID, Topic: topic, PrivateKey: key})
	if err != nil {
		return nil, err
	}
	return sender, nil
}

func runPushWorker(stop <-chan struct{}, controlServer *control.Server) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			workCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			if err := controlServer.DispatchPushDeliveries(workCtx, 50); err != nil {
				log.Printf("push delivery worker failed: %v", err)
			}
			cancel()
		}
	}
}

func loadProfiles(raw string) (map[string]control.RuntimeProfile, error) {
	profiles := map[string]control.RuntimeProfile{}
	if strings.TrimSpace(raw) == "" {
		return profiles, nil
	}
	if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
		return nil, errors.New("CYLO_RUNTIME_PROFILES must be a JSON object keyed by profile ID")
	}
	return profiles, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func splitEnv(name, fallback string) []string {
	value := env(name, fallback)
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func optionalSplitEnv(name string) []string {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func validBearer(value string) bool {
	bytes, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(bytes) == 32 && base64.RawURLEncoding.EncodeToString(bytes) == value
}
