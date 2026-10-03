package main

import (
	"fmt"

	"github.com/alexgorbatchev/agent-wrap/internal/logging"
	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/spf13/cobra"
)

var techCatalog = cobrahelptree.TechCatalog{
	"agent-wrap": {
		Summary:     "Identify agent projects, branches, and subtrees in a live terminal header",
		Description: "Identify agent projects, branches, and subtrees in a live terminal header - command-line interface.",
		Args:        []cobrahelptree.ArgSpec{{Name: "<agent>", Description: "Agent executable after --"}, {Name: "[args...]", Description: "Arguments passed unchanged to the agent"}},
	},
	"agent-wrap skill": {Summary: "Print the embedded agent usage reference"},
}

func setupHelp(cmd *cobra.Command) error {
	if err := cobrahelptree.SetupWithOptions(cmd, cobrahelptree.HelpOptions{
		Catalog: techCatalog,
		Tree:    cobrahelptree.TreeOptions{HideGeneratedCommands: true},
	}); err != nil {
		return err
	}
	help := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		if logging.IsAgentMode() {
			if _, err := fmt.Fprintln(c.OutOrStdout(), "ALERT: Agents must read `AGENT=1 agent-wrap skill` before using this tool."); err != nil {
				c.PrintErrln(err)
				return
			}
		}
		help(c, args)
	})
	return nil
}
