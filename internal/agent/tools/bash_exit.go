package tools

import (
	"encoding/json"
	"fmt"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// Shell exit codes with a single, shell-independent meaning: the requested
// program never started.
const (
	exitNotExecutable   = 126
	exitCommandNotFound = 127
)

// annotateBashExit adds a structured outcome to a finished foreground
// command. It never rewrites the command or its output, and it does not
// treat stderr text or an arbitrary non-zero code as failure: programs
// define their own exit meaning. It only marks the responses where the
// shell itself reports that the program could not be started.
func annotateBashExit(resp fantasy.ToolResponse, err error) (fantasy.ToolResponse, error) {
	if err != nil || resp.Metadata == "" {
		return resp, err
	}
	var meta BashResponseMetadata
	if json.Unmarshal([]byte(resp.Metadata), &meta) != nil || meta.ExitCode == nil || meta.Background {
		return resp, nil
	}
	meta.Executed = true
	switch code := *meta.ExitCode; code {
	case 0:
		meta.Status = "succeeded"
	case exitCommandNotFound:
		meta.Status = "command_not_found"
		resp.IsError = true
		resp.Content += fmt.Sprintf("\n\ncommand_not_found: the shell could not find the program (exit %d); the requested work was not performed. Install it, use an available tool, or report the blocker; the command was not retried or rewritten.", code)
	case exitNotExecutable:
		meta.Status = "not_executable"
		resp.IsError = true
		resp.Content += fmt.Sprintf("\n\nnot_executable: the program exists but could not be run (exit %d); the requested work was not performed.", code)
	default:
		meta.Status = "exited_nonzero"
	}
	if data, marshalErr := json.Marshal(meta); marshalErr == nil {
		resp.Metadata = string(data)
	}
	return resp, nil
}
