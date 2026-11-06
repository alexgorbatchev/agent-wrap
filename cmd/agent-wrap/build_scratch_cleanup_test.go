package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestBuildScratchFixtureWaitsForExitCleanup(t *testing.T) {
	_, script := buildScratchFixture(t)
	markers := t.TempDir()
	bin := t.TempDir()
	rm, err := exec.LookPath("rm")
	if err != nil {
		t.Fatal(err)
	}
	// Pause the real removal after the command group is gone. Readiness puts
	// fixture teardown inside the wrapper's EXIT trap instead of racing exit.
	delayedRemove := `#!/bin/sh
set -eu
trap 'printf interrupted > "$CLEANUP_MARKERS/interrupted"; exit 143' TERM
printf entered > "$CLEANUP_MARKERS/removing"
sleep 1
"$CLEANUP_RM" "$@"
printf 'cleanup complete\n'
`
	if err := os.WriteFile(filepath.Join(bin, "rm"), []byte(delayedRemove), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "bash", "-c", `set -eu
ps -o pgid= -p "$$" > "$1/group"
printf '%s\n' "$TMPDIR" > "$1/scratch"
`, "cleanup", markers)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"),
		"CLEANUP_MARKERS="+markers, "CLEANUP_RM="+rm)
	run := startScratchCommand(t, cmd, markers)
	scratchAwaitFile(t, filepath.Join(markers, "removing"))
	run.stop()
	if _, err := os.Stat(filepath.Join(markers, "interrupted")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture interrupted wrapper exit cleanup: %v", err)
	}
	run.wait(t, 0)
	if strings.TrimSpace(run.output.String()) != "cleanup complete" {
		t.Fatalf("wrapper output was not joined: %q", run.output.String())
	}
	scratch := scratchAwaitFile(t, filepath.Join(markers, "scratch"))
	if _, err := os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture cleanup left scratch: %v", err)
	}
	if err := syscall.Kill(-cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("wrapper group survived cleanup: %v", err)
	}
}
