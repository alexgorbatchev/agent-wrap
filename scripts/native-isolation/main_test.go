package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "scripts/native-isolation/run.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("set -eu\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func awaitFile(path string) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return errors.New("timed out waiting for " + path)
}

func cleanupGroup(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		data, err := os.ReadFile(filepath.Join(root, "group"))
		if err != nil {
			t.Error(err)
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Error(err)
			return
		}
		if err := sendGroup(pid, syscall.SIGKILL); err != nil {
			t.Error(err)
			return
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			err := syscall.Kill(-pid, 0)
			if errors.Is(err, syscall.ESRCH) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Error("test process group survived cleanup")
	})
}

func TestRunJoinsDescendants(t *testing.T) {
	for _, sig := range []syscall.Signal{0, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM} {
		t.Run(strconv.Itoa(int(sig)), func(t *testing.T) {
			t.Parallel()
			// The shell leader exits before its child. After TERM, that child
			// still writes inside scratch before reporting shutdown externally.
			root := fixture(t, `printf '%s\n' "$$" > "$1/group"
printf '%s\n' "$2" > "$1/scratch"
trap 'exit 143' TERM
bash -c '
set -eu
project=$1
scratch=$2
finish() {
    sleep 0.1
    printf retained > "$scratch/still-owned"
    printf finished > "$project/finished"
    exit 0
}
trap finish TERM
printf ready > "$1/ready"
if test "$3" = 0; then finish; fi
while :; do sleep 1; done
' child "$1" "$2" "`+strconv.Itoa(int(sig))+`" &
if test "`+strconv.Itoa(int(sig))+`" = 0; then exit 0; fi
wait
`)
			// Also join descendants when testing an intentionally broken runner.
			cleanupGroup(t, root)
			signals := make(chan os.Signal, 1)
			ready := make(chan error, 1)
			go func() {
				err := awaitFile(filepath.Join(root, "ready"))
				if err != nil {
					signals <- syscall.SIGTERM
				} else if sig != 0 {
					signals <- sig
				}
				ready <- err
			}()
			status, err := run(root, signals, time.Second)
			if readyErr := <-ready; readyErr != nil {
				t.Fatal(readyErr)
			}
			want := 0
			if sig != 0 {
				want = 128 + int(sig)
			}
			if err != nil || status != want {
				t.Fatalf("run = %d, %v; want %d", status, err, want)
			}
			if _, err := os.Stat(filepath.Join(root, "finished")); err != nil {
				t.Fatalf("descendant did not finish using scratch: %v", err)
			}
			entries, err := os.ReadDir(filepath.Join(root, ".tmp"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("scratch survived: %v, %v", entries, err)
			}
		})
	}
}

func TestMainExit(t *testing.T) {
	if got := mainExit(nil); got != 1 {
		t.Fatalf("missing project status = %d", got)
	}
	for _, status := range []int{0, 7} {
		root := fixture(t, "exit "+strconv.Itoa(status)+"\n")
		if got := mainExit([]string{root}); got != status {
			t.Fatalf("status = %d, want %d", got, status)
		}
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mainExit([]string{file}); got != 1 {
		t.Fatalf("invalid project status = %d", got)
	}
}

func TestRunStopsUnjoinedDescendant(t *testing.T) {
	root := fixture(t, `printf '%s\n' "$$" > "$1/group"
bash -c '
trap "" TERM
printf ready > "$1/ready"
exec sleep 60
' child "$1" &
while ! test -f "$1/ready"; do sleep 0.01; done
exit 0
`)
	cleanupGroup(t, root)
	status, err := run(root, nil, 50*time.Millisecond)
	if err != nil || status != 1 {
		t.Fatalf("run = %d, %v; want failed orphan shutdown", status, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("scratch survived confirmed forced shutdown: %v, %v", entries, err)
	}
}

func TestRunStartFailure(t *testing.T) {
	root := fixture(t, "exit 0\n")
	t.Setenv("PATH", t.TempDir())
	status, err := run(root, nil, time.Second)
	if err == nil || status != 1 {
		t.Fatalf("run = %d, %v; want launch error", status, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("scratch survived failed launch: %v, %v", entries, err)
	}
}
