package activity

import (
	"io"
	"os"
	"testing"
)

const (
	// agentHelperEnv makes the test binary stand in for an agent process.
	agentHelperEnv = "AGENT_WRAP_ACTIVITY_AGENT_HELPER"
	// agentReady is written once the helper runs, after exec renamed it.
	agentReady byte = '!'
)

func TestMain(m *testing.M) {
	if os.Getenv(agentHelperEnv) == "1" {
		if _, err := os.Stdout.Write([]byte{agentReady}); err != nil {
			os.Exit(1)
		}
		// Stay alive until the owning test kills the process or exits, closing stdin.
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
