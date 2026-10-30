package runner

import (
	"context"
	"github.com/ronigooja/Zoe/contracts"
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
