package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/alexgorbatchev/agent-wrap/internal/session"
	"github.com/spf13/cobra"
)

var version = "dev"

type childExit struct{ code int }

func (e *childExit) Error() string { return fmt.Sprintf("agent exited with status %d", e.code) }

func newRootCommand() (*cobra.Command, error) {
	opts := session.Options{ScanInterval: 2 * time.Second}
	cmd := &cobra.Command{
		Use: "agent-wrap -- <agent> [args...]", Short: "Identify agent projects, branches, and subtrees in a live terminal header",
		Version: version, SilenceUsage: true, SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.ArgsLenAtDash() < 0 {
				return nil
			}
			if cmd.ArgsLenAtDash() != 0 || len(args) == 0 {
				return errors.Join(fmt.Errorf("put the agent command after --, for example: agent-wrap -- claude"), cmd.Usage())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			code, err := session.Run(cmd.Context(), args, opts)
			if err != nil {
				return err
			}
			if code != 0 {
				return &childExit{code: code}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Harness, "harness", "", "Agent family for an alias: claude-code, pi, codex, or opencode")
	f.StringVar(&opts.Directory, "dir", "", "Launch directory (default: current directory)")
	f.StringVar(&opts.DefaultBranch, "default-branch", "", "Default branch when local origin/HEAD is unavailable")
	f.StringVar(&opts.Name, "name", "", "Agent label (default: the detected agent name)")
	f.StringVar(&opts.LogFile, "log-file", "", "Wrapper log file (default: a unique file under XDG state)")
	f.DurationVar(&opts.ScanInterval, "scan-interval", opts.ScanInterval, "Interval between agent discovery scans")
	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return errors.Join(err, cmd.Usage()) })
	cmd.AddCommand(newSkillCommand())
	if err := setupHelp(cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}
