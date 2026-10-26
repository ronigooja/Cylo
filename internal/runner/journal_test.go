package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
)

func TestSaveClaimReturnsPersistedPhaseAndProtectsDatabaseFiles(t *testing.T) {
	ctx := context.Background()
	filename := filepath.Join(t.TempDir(), "runner.db")
	journal, err := OpenJournal(ctx, filename)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	claim := contracts.JobClaim{RunID: "run-1", AttemptID: "attempt-1", Fence: 4}
	spec := ao.WorkerSpec{ClientRequestID: "run-1", ProjectID: "project-1", Harness: "codex", DisplayName: "worker", Prompt: "inspect"}
	if _, err := journal.SaveClaim(ctx, claim, spec, "create_pending"); err != nil {
		t.Fatal(err)
	}
	entry, err := journal.SaveClaim(ctx, claim, spec, "claimed")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Phase != "create_pending" {
		t.Fatalf("SaveClaim returned phase %q; want persisted create_pending", entry.Phase)
	}
	for _, name := range []string{filename, filename + "-wal", filename + "-shm"} {
		info, err := os.Stat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode is %04o; want 0600", filepath.Base(name), got)
		}
	}
}

func TestJournalLockRejectsSymlinkAndHardlinkAliases(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	filename := filepath.Join(dir, "runner.db")
	journal, err := OpenJournal(ctx, filename)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for _, test := range []struct {
		name  string
		setup func(string) error
	}{
		{name: "symlink", setup: func(alias string) error { return os.Symlink(filename, alias) }},
		{name: "hardlink", setup: func(alias string) error { return os.Link(filename, alias) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			alias := filepath.Join(dir, test.name+"-alias.db")
			if err := test.setup(alias); err != nil {
				t.Fatal(err)
			}
			second, err := OpenJournal(ctx, alias)
			if err == nil {
				_ = second.Close()
				t.Fatal("opened same journal through an alias")
			}
		})
	}
}

func TestDeploymentHostLockRejectsSecondRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.lock")
	first, err := OpenDeploymentHostLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := OpenDeploymentHostLock(path); err == nil {
		_ = second.Close()
		t.Fatal("second runner acquired the shared Docker host lock")
	}
}
