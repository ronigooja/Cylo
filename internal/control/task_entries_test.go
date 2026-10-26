package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/storage"
)

func taskEntriesTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err = store.DB().Exec(`INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at) VALUES('entries-device','default','entries-owner','test','approved',?,?)`, []byte("entries-device-hash"), now); err != nil {
		t.Fatal(err)
	}
	for _, task := range []struct{ id, owner string }{{"entries-task", "entries-owner"}, {"private-task", "private-owner"}} {
		if _, err = store.DB().Exec(`INSERT INTO tasks(id,workspace_id,title,goal,source,profile_id,status,version,created_at,updated_at,accepted_at,created_by_device_id,command_id,owner_subject_id) VALUES(?,'default','title','goal','user','worker','active',1,?,?,?,?,?,?)`, task.id, now, now, now, "entries-device", "cmd-"+task.id, task.owner); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{store: store}
	return server, func() { _ = store.Close() }
}

func getTaskEntries(server *Server, taskID, query, subject string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/v1/tasks/"+taskID+"/entries"+query, nil)
	r.SetPathValue("task_id", taskID)
	r = r.WithContext(context.WithValue(r.Context(), devicePrincipalKey{}, contracts.Principal{WorkspaceID: "default", SubjectID: subject, Scopes: []string{"client:read"}}))
	w := httptest.NewRecorder()
	server.handleTaskEntries(w, r)
	return w
}

func TestTaskEntriesPaginationUsesStableTaskScopedCursor(t *testing.T) {
	server, closeStore := taskEntriesTestServer(t)
	defer closeStore()
	for _, entry := range []struct{ id, stamp string }{
		{"entry-a", "2026-10-24T12:00:00Z"},
		{"entry-b", "2026-10-24T12:00:00Z"},
		{"entry-c", "2026-10-24T12:00:00.1Z"},
	} {
		if _, err := server.store.DB().Exec(`INSERT INTO task_entries(id,task_id,workspace_id,kind,content,author_device_id,created_at) VALUES(?,'entries-task','default','user_message',?,'entries-device',?)`, entry.id, entry.id, entry.stamp); err != nil {
			t.Fatal(err)
		}
	}
	first := getTaskEntries(server, "entries-task", "?limit=2", "entries-owner")
	if first.Code != 200 {
		t.Fatalf("first page status=%d body=%s", first.Code, first.Body.String())
	}
	var page taskEntriesPage
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 2 || page.Messages[0].ID != "entry-a" || page.Messages[1].ID != "entry-b" || page.NextCursor == nil {
		t.Fatalf("unexpected first page: %+v", page)
	}
	// Entries that sort before the cursor must not shift the next page.
	if _, err := server.store.DB().Exec(`INSERT INTO task_entries(id,task_id,workspace_id,kind,content,author_device_id,created_at) VALUES('entry-0','entries-task','default','user_message','earlier','entries-device','2026-10-23T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	second := getTaskEntries(server, "entries-task", "?limit=2&cursor="+*page.NextCursor, "entries-owner")
	if second.Code != 200 {
		t.Fatalf("second page status=%d body=%s", second.Code, second.Body.String())
	}
	var next taskEntriesPage
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Messages) != 1 || next.Messages[0].ID != "entry-c" || next.NextCursor != nil {
		t.Fatalf("unexpected second page: %+v", next)
	}
}

func TestTaskEntriesEnforcesTaskReadACLAndValidatesBounds(t *testing.T) {
	server, closeStore := taskEntriesTestServer(t)
	defer closeStore()
	denied := getTaskEntries(server, "private-task", "", "entries-owner")
	if denied.Code != 404 {
		t.Fatalf("unauthorized task status=%d body=%s", denied.Code, denied.Body.String())
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=bad", "?cursor=not-a-cursor"} {
		response := getTaskEntries(server, "entries-task", query, "entries-owner")
		if response.Code != 400 {
			t.Fatalf("query %q status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
}

func TestTaskEntriesOrdersInstantsAtNanosecondPrecision(t *testing.T) {
	server, closeStore := taskEntriesTestServer(t)
	defer closeStore()
	// Deliberately scramble IDs and vary precision and UTC offsets. The final
	// nanosecond must remain before the next second despite SQLite rounding.
	for _, entry := range []struct{ id, stamp string }{
		{"a-next-second", "2026-10-24T12:00:01Z"},
		{"b-last-nanosecond", "2026-10-24T12:00:00.999999999Z"},
		{"c-tenth", "2026-10-24T12:00:00.1Z"},
		{"d-nanosecond", "2026-10-24T12:00:00.000000001Z"},
		{"e-zero-offset", "2026-10-24T05:00:00-07:00"},
		{"f-zero-fraction", "2026-10-24T12:00:00.000Z"},
		{"g-zero", "2026-10-24T12:00:00Z"},
	} {
		if _, err := server.store.DB().Exec(`INSERT INTO task_entries(id,task_id,workspace_id,kind,content,author_device_id,created_at) VALUES(?,'entries-task','default','user_message',?,'entries-device',?)`, entry.id, entry.id, entry.stamp); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"e-zero-offset", "f-zero-fraction", "g-zero", "d-nanosecond", "c-tenth", "b-last-nanosecond", "a-next-second"}
	query := "?limit=1"
	for i, id := range want {
		response := getTaskEntries(server, "entries-task", query, "entries-owner")
		if response.Code != 200 {
			t.Fatalf("page %d: status=%d body=%s", i, response.Code, response.Body.String())
		}
		var page taskEntriesPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Messages) != 1 || page.Messages[0].ID != id {
			t.Fatalf("page %d: want %s, got %+v", i, id, page)
		}
		if i == len(want)-1 {
			if page.NextCursor != nil {
				t.Fatal("last page has a next cursor")
			}
		} else {
			if page.NextCursor == nil {
				t.Fatalf("page %d missing next cursor", i)
			}
			query = "?limit=1&cursor=" + *page.NextCursor
		}
	}
}

func TestTaskEntriesEmptyAuthenticationAndCursorScope(t *testing.T) {
	server, closeStore := taskEntriesTestServer(t)
	defer closeStore()
	empty := getTaskEntries(server, "entries-task", "", "entries-owner")
	if empty.Code != 200 || empty.Body.String() != "{\"messages\":[],\"next_cursor\":null}\n" {
		t.Fatalf("empty task: status=%d body=%s", empty.Code, empty.Body.String())
	}
	missing := getTaskEntries(server, "missing-task", "", "entries-owner")
	if missing.Code != 404 {
		t.Fatalf("missing task status=%d", missing.Code)
	}
	r := httptest.NewRequest("GET", "/v1/tasks/entries-task/entries", nil)
	r.SetPathValue("task_id", "entries-task")
	w := httptest.NewRecorder()
	server.handleTaskEntries(w, r)
	if w.Code != 401 {
		t.Fatalf("unauthenticated status=%d", w.Code)
	}
	for _, cursor := range []taskEntriesCursor{
		{Version: 1, TaskID: "another-task", CreatedAt: "2026-10-24T12:00:00Z", ID: "entry"},
		{Version: 2, TaskID: "entries-task", CreatedAt: "2026-10-24T12:00:00Z", ID: "entry"},
		{Version: 1, TaskID: "entries-task", CreatedAt: "bad", ID: "entry"},
		{Version: 1, TaskID: "entries-task", CreatedAt: "2026-10-24T12:00:00Z"},
	} {
		raw, err := encodeTaskEntriesCursor(cursor)
		if err != nil {
			t.Fatal(err)
		}
		response := getTaskEntries(server, "entries-task", "?cursor="+raw, "entries-owner")
		if response.Code != 400 {
			t.Fatalf("cursor %+v: status=%d body=%s", cursor, response.Code, response.Body.String())
		}
	}
}

func TestTaskEntriesHTTPRequiresDeviceReadScope(t *testing.T) {
	fixture, closeStore := taskEntriesTestServer(t)
	defer closeStore()
	// A synthetic bearer is stored only as a digest, matching device middleware.
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if _, _, err := InitializeInstanceAuth(context.Background(), fixture.store, token); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec(`UPDATE tasks SET owner_subject_id='owner' WHERE id='entries-task'`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		scopes []string
		want   int
	}{
		{"no grants", []string{}, 403},
		{"command only", []string{"client:command"}, 403},
		{"read", []string{"client:read"}, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, err := NewServer(fixture.store, Config{InstanceScopes: test.scopes})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/v1/tasks/entries-task/entries", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, test.want, w.Body.String())
			}
		})
	}
}
