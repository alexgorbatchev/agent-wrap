package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexgorbatchev/agent-wrap/internal/logging"
	"github.com/spf13/cobra"
)

func main() { os.Exit(execute()) }

func execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd, err := newRootCommand()
	if err != nil {
		if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
			return 1
		}
		return 1
	}
	cmd.SetContext(ctx)
	return run(cmd)
}

func run(cmd *cobra.Command) int {
	if err := cmd.Execute(); err != nil {
		var exit *childExit
		if errors.As(err, &exit) {
			return exit.code
		}
		prefix := "[ERROR]"
		if logging.IsAgentMode() {
			prefix = "ERR:"
		}
		if _, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "%s %v\n", prefix, err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}
