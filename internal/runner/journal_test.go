package runner

import (
	"context"
	"github.com/ronigooja/Zoe/contracts"
	"strings"
	"testing"
)

func TestJournalNeutralRuntimeFieldsRoundTrip(t *testing.T) {
	j := openTestJournal(t)
	c := testClaim()
	s := RuntimeSpec{RunID: c.RunID, AttemptID: c.AttemptID, Fence: "1", Model: "model", Prompt: "prompt"}
	if _, err := j.SaveClaim(context.Background(), c, s, "claimed"); err != nil {
		t.Fatal(err)
	}
	if err := j.SetSession(context.Background(), c.RunID, c.AttemptID, c.Fence, "session", "bound"); err != nil {
		t.Fatal(err)
	}
	if err := j.SetContainer(context.Background(), c.RunID, c.AttemptID, c.Fence, "container"); err != nil {
		t.Fatal(err)
	}
	e, ok, err := j.Entry(context.Background(), c.RunID)
	if err != nil || !ok || e.Session != "session" || e.Container != "container" {
		t.Fatalf("entry=%+v ok=%v err=%v", e, ok, err)
	}
	_ = contracts.RunnerObservation{RuntimeSessionID: e.Session, RuntimeContainerID: e.Container}
}

func TestJournalLoadsLegacyAOFieldsThroughNeutralEntry(t *testing.T) {
	j := openTestJournal(t)
	c := testClaim()
	s := RuntimeSpec{RunID: c.RunID, AttemptID: c.AttemptID, Fence: "1", Model: "model", Prompt: "prompt"}
	if _, err := j.SaveClaim(context.Background(), c, s, "claimed"); err != nil {
		t.Fatal(err)
	}
	if _, err := j.db.Exec(`UPDATE journal_runs SET runtime_spec_json='{}',runtime_session_id='',runtime_container_id='',ao_session_id='legacy-session',ao_container_id='` + strings.Repeat("a", 64) + `'`); err != nil {
		t.Fatal(err)
	}
	e, ok, err := j.Entry(context.Background(), c.RunID)
	if err != nil || !ok || e.Session != "legacy-session" || e.Container != strings.Repeat("a", 64) {
		t.Fatalf("legacy entry=%+v ok=%v err=%v", e, ok, err)
	}
}
