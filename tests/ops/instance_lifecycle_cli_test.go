package ops_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The lifecycle commands use only Python's standard library. Their regression
// suite exercises temporary owned roots and fake executors; it never contacts
// systemd, a running deployment, or any external store.
func TestInstanceLifecycleOperatorCLIs(t *testing.T) {
	root := repositoryRoot(t)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 is required for lifecycle CLI regression coverage")
	}
	command := exec.Command(python, "-m", "unittest", "tools.test_instance_lifecycle_cli")
	command.Dir = filepath.Clean(root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("operator lifecycle CLI regression: %v\n%s", err, output)
	}
}
