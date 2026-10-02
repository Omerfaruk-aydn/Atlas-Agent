package config

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutionPolicyValidation(t *testing.T) {
	t.Parallel()
	path, err := os.Executable()
	require.NoError(t, err)
	cfg := &Config{Options: &Options{Execution: &Execution{Mode: "container-required", RuntimePath: path, Image: "fixture/image@sha256:" + strings.Repeat("a", 64)}}}
	require.NoError(t, cfg.ValidateExecution())
	cfg.Options.Execution.Network = "host"
	require.Error(t, cfg.ValidateExecution())
	cfg.Options.Execution.Network = "none"
	cfg.Options.Execution.Image = "fixture:latest"
	require.Error(t, cfg.ValidateExecution())
}
