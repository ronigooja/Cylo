package acceptance

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/internal/capabilities"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/memory"
	"github.com/ronigooja/Cylo/internal/storage"
	"github.com/ronigooja/Cylo/internal/voice"
)

var exportDeviceScopes = []string{"client:read", "client:command", "export:read", "memory:read:personal", "memory:read:workspace"}

func scopedExportServer(t *testing.T) (*storage.Store, http.Handler, string, string, string) {
	t.Helper()
	code, token := reviewToken(t), reviewToken(t)
	store, err := storage.Open(context.Background(), storage.Config{Path: filepath.Join(t.TempDir(), "scoped-export.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(store, control.Config{BootstrapCode: code, DeviceScopes: exportDeviceScopes})
	if err != nil {
		t.Fatal(err)
	}
	if err = memory.Register(server, store); err != nil {
		t.Fatal(err)
	}
	if err = voice.RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = capabilities.RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterExportRoutes(server); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := server.Handler()
	registration := reviewBootstrap(t, h, code, token)
	var subject string
	if err = store.DB().QueryRow(`SELECT subject_id FROM devices WHERE id=?`, registration.Device.ID).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	return store, h, token, subject, registration.Device.ID
}

func TestScopedExportHTTPHonorsMembershipMemoryVersionsAndTombstones(t *testing.T) {
	store, handler, token, subject, deviceID := scopedExportServer(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seedExportProjectTask(t, store.DB(), "visible-project", "visible-task", "default", "other-subject", deviceID, now)
	if _, err := store.DB().Exec(`INSERT INTO project_memberships(workspace_id,project_id,subject_id,role,status,created_at) VALUES('default','visible-project',?,'viewer','active',?)`, subject, now); err != nil {
		t.Fatal(err)
	}
	seedExportProjectTask(t, store.DB(), "hidden-project", "hidden-task", "default", "someone-else", deviceID, now)
	if _, err := store.DB().Exec(`INSERT INTO workspaces(id,name,created_at) VALUES('other-workspace','private',?)`, now); err != nil {
		t.Fatal(err)
	}
	seedExportProjectTask(t, store.DB(), "cross-workspace-project", "cross-workspace-task", "other-workspace", subject, deviceID, now)
	seedExportMemory(t, store.DB(), "personal-live", "personal", "", subject, "private-note", "old revision", "current revision", false, now)
	seedExportMemory(t, store.DB(), "personal-deleted", "personal", "", subject, "deleted-note", "must not survive", "", true, now)
	seedExportMemory(t, store.DB(), "other-personal", "personal", "", "someone-else", "other-note", "other user's text", "", false, now)
	seedExportMemory(t, store.DB(), "workspace-live", "workspace", "", "", "shared-note", "workspace memory", "", false, now)
	seedExportMemory(t, store.DB(), "project-live", "project", "visible-project", "", "project-note", "visible project memory", "", false, now)
	seedExportMemory(t, store.DB(), "project-hidden", "project", "hidden-project", "", "hidden-note", "hidden project memory", "", false, now)

	personal := reviewRequest(handler, http.MethodGet, "/v1/export?scope=personal", token, "", nil)
	if personal.Code != http.StatusOK {
		t.Fatalf("personal export=%d %s", personal.Code, personal.Body.String())
	}
	if personal.Header().Get("Content-Type") != "application/vnd.cylo.export+zip" || !strings.HasPrefix(personal.Header().Get("Content-Disposition"), `attachment; filename="cylo-export-`) || personal.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe export response headers: %#v", personal.Header())
	}
	if err := assertExportAuditMatchesBody(store.DB(), personal.Body.Bytes(), personal.Header().Get("Content-Length")); err != nil {
		t.Fatal(err)
	}
	manifest, personalDoc := decodeExportPackage(t, personal.Body.Bytes(), personal.Header())
	assertExportAuditProof(t, store.DB(), manifest)
	personalText := personal.Body.String()
	for _, mustHave := range []string{"visible-project", "visible-task", "personal-live", "old revision", "current revision", "entry-visible-task"} {
		if !strings.Contains(personalText, mustHave) {
			t.Errorf("personal export omitted %q", mustHave)
		}
	}
	for _, mustOmit := range []string{"hidden-project", "hidden-task", "cross-workspace-task", "personal-deleted", "must not survive", "other-personal", "other user's text", "workspace-live", "workspace memory", "project-live"} {
		if strings.Contains(personalText, mustOmit) {
			t.Errorf("personal export disclosed %q", mustOmit)
		}
	}
	if manifest["format"] != "cylo.authority-export" || manifest["schema_version"] != float64(2) || manifest["scope"] != "personal" {
		t.Fatalf("unstable manifest: %#v", manifest)
	}
	memories := personalDoc["memory"].([]any)
	var liveMemory map[string]any
	for _, item := range memories {
		candidate := item.(map[string]any)
		if candidate["id"] == "personal-live" {
			liveMemory = candidate
		}
	}
	if liveMemory == nil {
		t.Fatal("live personal memory record missing from parsed export")
	}
	revisions := liveMemory["revisions"].([]any)
	if len(revisions) != 2 {
		t.Fatalf("memory history revisions=%d, want 2", len(revisions))
	}
	firstRevision := revisions[0].(map[string]any)
	if firstRevision["revision"] != float64(1) || len(firstRevision["sources"].([]any)) != 1 {
		t.Fatalf("versioned source provenance was not portable: %#v", firstRevision)
	}

	workspace := reviewRequest(handler, http.MethodGet, "/v1/export?scope=workspace", token, "", nil)
	if workspace.Code != http.StatusOK {
		t.Fatalf("workspace export=%d %s", workspace.Code, workspace.Body.String())
	}
	for _, mustHave := range []string{"visible-project", "visible-task", "workspace-live", "workspace memory", "project-live", "visible project memory"} {
		if !strings.Contains(workspace.Body.String(), mustHave) {
			t.Errorf("workspace export omitted %q", mustHave)
		}
	}
	for _, mustOmit := range []string{"hidden-project", "hidden-task", "cross-workspace-task", "project-hidden", "hidden project memory", "personal-live", "personal private-note"} {
		if strings.Contains(workspace.Body.String(), mustOmit) {
			t.Errorf("workspace export disclosed %q", mustOmit)
		}
	}
	workspaceManifest, _ := decodeExportPackage(t, workspace.Body.Bytes(), workspace.Header())
	assertExportAuditProof(t, store.DB(), workspaceManifest)
	if workspaceManifest["omissions"] == nil || workspaceManifest["authority"] == nil || workspaceManifest["integrity"] == nil {
		t.Fatal("coverage, authority, or integrity metadata missing from manifest")
	}

	var audits int
	if err := store.DB().QueryRow(`SELECT count(*) FROM events WHERE event_type='data.export.prepared' AND subject_id=?`, subject).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("prepared export audit count=%d err=%v", audits, err)
	}
}

func assertExportAuditProof(t *testing.T, db *sql.DB, manifest map[string]any) {
	t.Helper()
	authority, ok := manifest["authority"].(map[string]any)
	if !ok {
		t.Fatalf("export manifest authority is malformed: %#v", manifest["authority"])
	}
	snapshotCursor, err := strconv.ParseInt(authority["snapshot_event_cursor"].(string), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	validatedCursor, err := strconv.ParseInt(authority["validated_through_cursor"].(string), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	auditCursor, err := strconv.ParseInt(authority["export_audit_event_cursor"].(string), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	exportID, ok := manifest["export_id"].(string)
	if !ok || validatedCursor != snapshotCursor+1 || auditCursor != validatedCursor || authority["export_audit_event_id"] != exportID || authority["export_audit_event_type"] != "data.export.prepared" {
		t.Fatalf("manifest does not attest the single prepared audit cursor: %#v", authority)
	}
	var eventID, eventType, aggregateID string
	if err = db.QueryRow(`SELECT event_id,event_type,aggregate_id FROM events WHERE cursor=?`, auditCursor).Scan(&eventID, &eventType, &aggregateID); err != nil {
		t.Fatal(err)
	}
	if eventID != exportID || eventType != authority["export_audit_event_type"] || aggregateID != exportID {
		t.Fatalf("manifest audit proof disagrees with event: id=%s type=%s aggregate=%s", eventID, eventType, aggregateID)
	}
}

func TestScopedExportUsesActiveProjectACLAndRedactsHiddenReferences(t *testing.T) {
	store, handler, token, subject, device := scopedExportServer(t)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	seedExportProjectTask(t, store.DB(), "archived-project", "archived-task", "default", subject, device, at)
	if _, err := store.DB().Exec(`UPDATE projects SET status='archived' WHERE id='archived-project'`); err != nil {
		t.Fatal(err)
	}
	seedExportMemory(t, store.DB(), "hidden-personal-memory", "personal", "", "another-subject", "hidden", "hidden memory", "", false, at)
	seedExportMemory(t, store.DB(), "visible-personal-memory", "personal", "", subject, "visible", "visible memory", "", false, at)
	if _, err := store.DB().Exec(`UPDATE memory_claims SET supersedes_id='hidden-personal-memory' WHERE id='visible-personal-memory'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_sources(workspace_id,id,version,scope_kind,scope_id,principal_id,kind,reference,content_hash,recorded_at,producer) VALUES('default','visible-hidden-ref-source',1,'personal','',?,'task_entry','task_entry:archived-entry','',?,'fixture')`, subject, at); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_revision_sources(claim_id,revision,workspace_id,source_id,source_version) VALUES('visible-personal-memory',1,'default','visible-hidden-ref-source',1)`); err != nil {
		t.Fatal(err)
	}
	response := reviewRequest(handler, http.MethodGet, "/v1/export?scope=personal", token, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export=%d %s", response.Code, response.Body.String())
	}
	for _, hidden := range []string{"archived-project", "hidden-personal-memory", "task_entry:archived-entry"} {
		if strings.Contains(response.Body.String(), hidden) {
			t.Errorf("export disclosed inactive or inaccessible reference %q", hidden)
		}
	}
	if !strings.Contains(response.Body.String(), "visible memory") {
		t.Fatal("authorized memory omitted")
	}
}

func TestScopedExportSizeLimitAndTemporaryCleanup(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	store, handler, token, subject, device := scopedExportServer(t)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	seedExportProjectTask(t, store.DB(), "large-project", "large-task", "default", subject, device, at)
	largeField := strings.Repeat("x", 520<<10)
	for i := 0; i < 130; i++ {
		id := fmt.Sprintf("large-task-%03d", i)
		if _, err := store.DB().Exec(`INSERT INTO tasks(id,workspace_id,project_id,title,goal,source,profile_id,status,version,created_at,updated_at,accepted_at,created_by_device_id,command_id,owner_subject_id,lineage_root_task_id,lineage_depth,child_policy_json,root_child_limit) VALUES(?,'default','large-project',?,?, 'user','profile','complete',1,?,?,?,?,?,?,?,0,'{}',0)`, id, id, largeField, at, at, at, device, "command-"+id, subject, id); err != nil {
			t.Fatal(err)
		}
	}
	response := reviewRequest(handler, http.MethodGet, "/v1/export?scope=workspace", token, "", nil)
	if response.Code != http.StatusOK || response.Body.Len() <= 64<<20 {
		t.Fatalf("streamed export=%d body length=%d body=%s", response.Code, response.Body.Len(), response.Body.String())
	}
	decodeExportPackage(t, response.Body.Bytes(), response.Header())
	leftovers, err := filepath.Glob(filepath.Join(tempRoot, "cylo-export-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary export remains after bounded stream: %v err=%v", leftovers, err)
	}
	var audits int
	if err = store.DB().QueryRow(`SELECT count(*) FROM events WHERE event_type='data.export.prepared'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("streamed export audit=%d err=%v", audits, err)
	}
}

func TestScopedExportRejectsOversizedFieldBeforePreparingArchive(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	store, handler, token, subject, device := scopedExportServer(t)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	seedExportProjectTask(t, store.DB(), "oversized-project", "oversized-task", "default", subject, device, at)
	if _, err := store.DB().Exec(`UPDATE tasks SET goal=? WHERE id='oversized-task'`, strings.Repeat("x", (1<<20)+1)); err != nil {
		t.Fatal(err)
	}
	response := reviewRequest(handler, http.MethodGet, "/v1/export?scope=workspace", token, "", nil)
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "export_field_too_large") {
		t.Fatalf("oversized field export=%d %s", response.Code, response.Body.String())
	}
	leftovers, err := filepath.Glob(filepath.Join(tempRoot, "cylo-export-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary export remains after field-bound rejection: %v err=%v", leftovers, err)
	}
	var audits int
	if err = store.DB().QueryRow(`SELECT count(*) FROM events WHERE event_type='data.export.prepared'`).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("rejected export audit=%d err=%v", audits, err)
	}
}

func TestScopedExportClientCancelCleansTemporaryFile(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	store, handler, token, subject, device := scopedExportServer(t)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	seedExportProjectTask(t, store.DB(), "cancel-project", "cancel-task", "default", subject, device, at)
	if _, err := store.DB().Exec(`UPDATE tasks SET goal=? WHERE id='cancel-task'`, strings.Repeat("x", 32<<20)); err != nil {
		t.Fatal(err)
	}
	live := httptest.NewServer(handler)
	t.Cleanup(live.Close)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, live.URL+"/v1/export?scope=workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := live.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = response.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		leftovers, globErr := filepath.Glob(filepath.Join(tempRoot, "cylo-export-*"))
		if globErr != nil {
			t.Fatal(globErr)
		}
		if len(leftovers) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	leftovers, _ := filepath.Glob(filepath.Join(tempRoot, "cylo-export-*"))
	t.Fatalf("temporary export remains after canceled response: %v", leftovers)
}

func assertExportAuditMatchesBody(db *sql.DB, body []byte, contentLength string) error {
	n, err := strconv.Atoi(contentLength)
	if err != nil {
		return fmt.Errorf("invalid content length %q: %w", contentLength, err)
	}
	if n != len(body) {
		return fmt.Errorf("content length=%d body bytes=%d", n, len(body))
	}
	var raw []byte
	err = db.QueryRow(`SELECT payload FROM events WHERE event_type='data.export.prepared' ORDER BY cursor DESC LIMIT 1`).Scan(&raw)
	if err != nil {
		return err
	}
	var event struct {
		Payload struct {
			Bytes  int64  `json:"archive_bytes"`
			SHA256 string `json:"archive_sha256"`
		} `json:"payload"`
	}
	if err = json.Unmarshal(raw, &event); err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	if event.Payload.Bytes != int64(len(body)) || event.Payload.SHA256 != hex.EncodeToString(digest[:]) {
		return fmt.Errorf("audit does not match actual export: bytes=%d hash=%s actual_bytes=%d actual_hash=%s", event.Payload.Bytes, event.Payload.SHA256, len(body), hex.EncodeToString(digest[:]))
	}
	return nil
}

func decodeExportPackage(t *testing.T, body []byte, headers http.Header) (map[string]any, map[string]any) {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("export ZIP is invalid: %v", err)
	}
	if len(archive.File) != 2 || archive.File[0].Name != "manifest.json" || archive.File[1].Name != "data.json" || archive.File[0].Method != zip.Store || archive.File[1].Method != zip.Store {
		t.Fatalf("unexpected export archive entries: %#v", archive.File)
	}
	readEntry := func(file *zip.File) []byte {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	manifestBytes, dataBytes := readEntry(archive.File[0]), readEntry(archive.File[1])
	archiveHash := sha256.Sum256(body)
	manifestHash := sha256.Sum256(manifestBytes)
	if headers.Get("X-Cylo-Archive-SHA256") != hex.EncodeToString(archiveHash[:]) || headers.Get("X-Cylo-Manifest-SHA256") != hex.EncodeToString(manifestHash[:]) {
		t.Fatal("export package hash headers do not match bytes")
	}
	var manifest, data map[string]any
	if err = json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	if err = json.Unmarshal(dataBytes, &data); err != nil {
		t.Fatalf("data JSON: %v", err)
	}
	integrity := manifest["integrity"].(map[string]any)
	dataHash := sha256.Sum256(dataBytes)
	if integrity["data_sha256"] != hex.EncodeToString(dataHash[:]) || integrity["data_bytes"] != float64(len(dataBytes)) {
		t.Fatal("manifest data integrity does not match data.json")
	}
	return manifest, data
}

func TestScopedExportRequiresSeparateExportAndMemoryReadGrants(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	store, err := storage.Open(context.Background(), storage.Config{Path: filepath.Join(t.TempDir(), "export-scope.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(store, control.Config{BootstrapCode: code, DeviceScopes: []string{"client:read", "memory:read:personal", "memory:read:workspace"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = memory.Register(server, store); err != nil {
		t.Fatal(err)
	}
	if err = voice.RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = capabilities.RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterExportRoutes(server); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := server.Handler()
	reviewBootstrap(t, h, code, token)
	resp := reviewRequest(h, http.MethodGet, "/v1/export?scope=personal", token, "", nil)
	if resp.Code != http.StatusForbidden || strings.Contains(resp.Body.String(), "sqlite") {
		t.Fatalf("missing export grant was not safely denied: %d %s", resp.Code, resp.Body.String())
	}
	resp = reviewRequest(h, http.MethodGet, "/v1/export?scope=invalid", token, "", nil)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope status=%d", resp.Code)
	}
}

func TestScopedExportKeepsNarrowMemoryGrantNarrow(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	store, err := storage.Open(context.Background(), storage.Config{Path: filepath.Join(t.TempDir(), "export-narrow.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(store, control.Config{BootstrapCode: code, DeviceScopes: []string{"client:read", "export:read", "memory:read:project:project-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = memory.Register(server, store); err != nil {
		t.Fatal(err)
	}
	if err = voice.RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = capabilities.RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterExportRoutes(server); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := server.Handler()
	registration := reviewBootstrap(t, h, code, token)
	var subject string
	if err = store.DB().QueryRow(`SELECT subject_id FROM devices WHERE id=?`, registration.Device.ID).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seedExportProjectTask(t, store.DB(), "project-a", "task-a", "default", "other-owner", registration.Device.ID, now)
	if _, err = store.DB().Exec(`INSERT INTO project_memberships(workspace_id,project_id,subject_id,role,status,created_at) VALUES('default','project-a',?,'viewer','active',?)`, subject, now); err != nil {
		t.Fatal(err)
	}
	seedExportMemory(t, store.DB(), "allowed-project-memory", "project", "project-a", "", "allowed", "allowed project text", "", false, now)
	seedExportMemory(t, store.DB(), "denied-project-memory", "project", "project-b", "", "denied", "denied project text", "", false, now)
	seedExportMemory(t, store.DB(), "denied-workspace-memory", "workspace", "", "", "workspace", "denied workspace text", "", false, now)
	resp := reviewRequest(h, http.MethodGet, "/v1/export?scope=workspace", token, "", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("narrow workspace export=%d %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "allowed project text") {
		t.Fatal("authorized project memory was omitted")
	}
	for _, denied := range []string{"denied project text", "denied workspace text"} {
		if strings.Contains(resp.Body.String(), denied) {
			t.Errorf("narrow memory grant disclosed %q", denied)
		}
	}
}

func TestScopedExportConcurrentWritesReturnOneSnapshotOrSafeRetry(t *testing.T) {
	store, handler, token, subject, deviceID := scopedExportServer(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seedExportProjectTask(t, store.DB(), "concurrent-project", "concurrent-task", "default", subject, deviceID, now)
	// Many bounded rows give the writer a repeatable window in which to race an
	// authoritative update while the handler builds its on-disk file.
	large := strings.Repeat("x", 512<<10)
	for i := 0; i < 24; i++ {
		id := fmt.Sprintf("concurrent-task-%02d", i)
		if _, err := store.DB().Exec(`INSERT INTO tasks(id,workspace_id,project_id,title,goal,source,profile_id,status,version,created_at,updated_at,accepted_at,created_by_device_id,command_id,owner_subject_id,lineage_root_task_id,lineage_depth,child_policy_json,root_child_limit) VALUES(?,'default','concurrent-project',?,?, 'user','profile','complete',1,?,?,?,?,?,?,?,0,'{}',0)`, id, id, large, now, now, now, deviceID, "command-"+id, subject, id); err != nil {
			t.Fatal(err)
		}
	}
	live := httptest.NewServer(handler)
	t.Cleanup(live.Close)
	var wg sync.WaitGroup
	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, _ := http.NewRequest(http.MethodGet, live.URL+"/v1/export?scope=personal", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := live.Client().Do(req)
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()
	changed := false
	for i := 0; i < 32; i++ {
		_, err := store.DB().Exec(`UPDATE tasks SET result=?,version=version+1,updated_at=? WHERE id='concurrent-task'`, fmt.Sprintf("revision-%d", i), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.DB().Exec(`UPDATE event_counters SET current_cursor=current_cursor+1 WHERE workspace_id='default'`); err != nil {
			t.Fatal(err)
		}
		changed = true
	}
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
	resp := <-respCh
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("concurrent fixture did not update")
	}
	if resp.StatusCode == http.StatusOK {
		_, document := decodeExportPackage(t, body, resp.Header)
		tasks := document["tasks"].([]any)
		if len(tasks) != 1 {
			t.Fatalf("snapshot task count=%d", len(tasks))
		}
		got := tasks[0].(map[string]any)
		if got["result"] != "" && !strings.HasPrefix(got["result"].(string), "revision-") {
			t.Fatalf("mixed task version in export: %#v", got)
		}
	} else if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "export_snapshot_stale") {
		t.Fatalf("concurrent authority change had unsafe outcome: status=%d body=%s", resp.StatusCode, body)
	}
}

func seedExportProjectTask(t *testing.T, db *sql.DB, projectID, taskID, workspace, owner, deviceID, at string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO projects(id,workspace_id,name,description,status,version,created_at,updated_at,owner_subject_id) VALUES(?,?,?,'','active',1,?,?,?)`, projectID, workspace, projectID, at, at, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks(id,workspace_id,project_id,title,goal,source,profile_id,status,version,created_at,updated_at,accepted_at,created_by_device_id,command_id,owner_subject_id,lineage_root_task_id,lineage_depth,child_policy_json,root_child_limit) VALUES(?,?,?,?,?,'user','profile','complete',1,?,?,?,?,?,?,?,0,'{}',0)`, taskID, workspace, projectID, taskID, "goal-"+taskID, at, at, at, deviceID, "command-"+taskID, owner, taskID); err != nil {
		t.Fatal(err)
	}
	entryID := "entry-" + taskID
	if _, err := db.Exec(`INSERT INTO task_entries(id,task_id,workspace_id,kind,content,author_device_id,created_at) VALUES(?,?,?,'user_message',?,?,?)`, entryID, taskID, workspace, "entry-"+taskID, deviceID, at); err != nil {
		t.Fatal(err)
	}
}

func seedExportMemory(t *testing.T, db *sql.DB, id, scope, scopeID, principal, key, firstContent, currentContent string, deleted bool, at string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO memory_sources(workspace_id,id,version,scope_kind,scope_id,principal_id,kind,reference,content_hash,recorded_at,producer) VALUES('default',?,1,?,?,?,'user_note',?,?,?,'user')`, "source-"+id, scope, scopeID, principal, "command:source-"+id, "", at); err != nil {
		t.Fatal(err)
	}
	status, revision := "active", 1
	if currentContent != "" {
		revision = 2
	}
	if deleted {
		status = "tombstoned"
		revision++
	}
	var deletedAt any
	if deleted {
		deletedAt = at
	}
	if _, err := db.Exec(`INSERT INTO memory_claims(id,workspace_id,scope_kind,scope_id,principal_id,subject,claim_key,authority_kind,status,revision,created_at,updated_at,deleted_at) VALUES(?,'default',?,?,?,'export test',?,'user_confirmed',?,?,?, ?,?)`, id, scope, scopeID, principal, key, status, revision, at, at, deletedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) VALUES(?,1,?,'user_confirmed','superseded',?,'export-fixture')`, id, firstContent, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memory_revision_sources(claim_id,revision,workspace_id,source_id,source_version) VALUES(?,1,'default',?,1)`, id, "source-"+id); err != nil {
		t.Fatal(err)
	}
	if currentContent != "" {
		if _, err := db.Exec(`INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) VALUES(?,2,?,'user_confirmed',?,?, 'export-fixture')`, id, currentContent, status, at); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO memory_revision_sources(claim_id,revision,workspace_id,source_id,source_version) VALUES(?,2,'default',?,1)`, id, "source-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if deleted {
		if _, err := db.Exec(`DELETE FROM memory_revision_sources WHERE claim_id=?`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`DELETE FROM memory_revisions WHERE claim_id=?`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO memory_revisions(claim_id,revision,content,authority_kind,status,recorded_at,actor_id) VALUES(?,?,'','user_confirmed','tombstoned',?,'export-fixture')`, id, revision, at); err != nil {
			t.Fatal(err)
		}
	}
}
