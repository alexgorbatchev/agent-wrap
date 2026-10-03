package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/creack/pty"
	"github.com/spf13/cobra"
	ghostty "go.mitchellh.com/libghostty"
)

func executeCommand(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	cmd, err := newRootCommand()
	if err != nil {
		return "", err
	}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return buf.String(), err
}

func rootCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd, err := newRootCommand()
	if err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestRun(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		args       []string
		code       int
		want       string
	}{
		{"version", "0", []string{"--version"}, 0, version + "\n"},
		{"human argument error", "0", []string{"claude"}, 1, "[ERROR] put the agent command after --"},
		{"agent argument error", "1", []string{"claude"}, 1, "ERR: put the agent command after --"},
		{"no child", "1", []string{"--"}, 1, "ERR: put the agent command after --"},
		{"unknown executable", "1", []string{"--", "unknown"}, 1, "ERR: unknown agent"},
		{"pass through version", "1", []string{"--", "claude", "--version"}, 1, "ERR: agent-wrap requires an interactive terminal"},
		{"flag error", "1", []string{"--unknown"}, 1, "ERR: unknown flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENT", tc.mode)
			cmd := rootCommand(t)
			var out, diagnostic bytes.Buffer
			cmd.SetArgs(tc.args)
			cmd.SetOut(&out)
			cmd.SetErr(&diagnostic)
			if code := run(cmd); code != tc.code {
				t.Fatalf("code=%d want=%d", code, tc.code)
			}
			if tc.code == 0 {
				if out.String() != tc.want || diagnostic.Len() != 0 {
					t.Fatal(out.String(), diagnostic.String())
				}
			} else if !strings.Contains(diagnostic.String(), tc.want) {
				t.Fatalf("diagnostic=%q", diagnostic.String())
			}
		})
	}
}

func TestExecuteEntryPoint(t *testing.T) {
	previous := os.Args
	os.Args = []string{"agent-wrap", "--version"}
	t.Cleanup(func() { os.Args = previous })
	if code := execute(); code != 0 {
		t.Fatalf("execute=%d", code)
	}
}

func TestBareHelpAndDiagnosticWriteFailure(t *testing.T) {
	out, err := executeCommand()
	if err != nil || !strings.Contains(out, "agent-wrap -- <agent>") {
		t.Fatalf("help=%q err=%v", out, err)
	}
	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := rootCommand(t)
	cmd.SetArgs([]string{"--unknown"})
	cmd.SetErr(closed)
	if run(cmd) != 1 {
		t.Fatal("write failure lost error exit")
	}
}

func TestCommandPreservesChildExitInTerminal(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	previousInput, previousOutput := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = slave, slave
	drained := make(chan struct{})
	emulator, err := ghostty.NewTerminal(ghostty.WithSize(80, 24), ghostty.WithWritePty(func(_ *ghostty.Terminal, data []byte) {
		if _, err := master.Write(data); err != nil {
			t.Error(err)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Stdin, os.Stdout = previousInput, previousOutput
		if err := slave.Close(); err != nil {
			t.Error(err)
		}
		if err := master.Close(); err != nil {
			t.Error(err)
		}
		<-drained
		emulator.Close()
	})
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("PI_SESSIONS_DIR", t.TempDir())
	go func() {
		defer close(drained)
		buf := make([]byte, 65536)
		for {
			n, err := master.Read(buf)
			if err != nil {
				if !errors.Is(err, os.ErrClosed) && !errors.Is(err, syscall.EIO) {
					t.Error(err)
				}
				return
			}
			if _, err := emulator.Write(buf[:n]); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for _, tc := range []struct {
		script string
		code   int
	}{{"exit 0", 0}, {"exit 37", 37}} {
		cmd := rootCommand(t)
		cmd.SetArgs([]string{"--harness", "pi", "--", "sh", "-c", tc.script})
		var diagnostic bytes.Buffer
		cmd.SetErr(&diagnostic)
		if code := run(cmd); code != tc.code || diagnostic.Len() != 0 {
			t.Fatalf("%q: code=%d diagnostic=%q", tc.script, code, diagnostic.String())
		}
	}
}

func TestRunChildExit(t *testing.T) {
	if (&childExit{code: 37}).Error() != "agent exited with status 37" {
		t.Fatal("incorrect status description")
	}
}

func TestCancelledCommand(t *testing.T) {
	cmd := rootCommand(t)
	cmd.SetArgs([]string{"--", "claude"})
	cmd.SetErr(new(bytes.Buffer))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	if run(cmd) != 1 {
		t.Fatal("ignored cancellation")
	}
}
