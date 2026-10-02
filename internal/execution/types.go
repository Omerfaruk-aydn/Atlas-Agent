package execution

import (
	"context"
	"io"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type ExecutionPolicy struct {
	Mode            string   `json:"mode"`
	RuntimePath     string   `json:"runtime_path"`
	Image           string   `json:"image"`
	Network         string   `json:"network"`
	ReadOnly        bool     `json:"read_only"`
	CPUs            float64  `json:"cpus"`
	MemoryBytes     int64    `json:"memory_bytes"`
	MaxProcesses    int      `json:"max_processes"`
	TimeoutMS       int64    `json:"timeout_ms"`
	EnvironmentKeys []string `json:"environment_keys"`
}

type Request struct {
	ToolCallID string          `json:"tool_call_id,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	RunID      string          `json:"run_id"`
	Root       string          `json:"root"`
	TaskID     string          `json:"task_id"`
	Argv       []string        `json:"argv"`
	Env        []string        `json:"-"`
	Stdin      io.Reader       `json:"-"`
	Policy     ExecutionPolicy `json:"policy"`
}

type Result struct {
	RunID       string                  `json:"run_id"`
	Backend     string                  `json:"backend"`
	ContainerID string                  `json:"container_id"`
	HostOS      string                  `json:"host_os"`
	ExecutionOS string                  `json:"execution_os"`
	Status      string                  `json:"status"`
	ExitCode    *int                    `json:"exit_code,omitempty"`
	OutputHash  string                  `json:"output_hash"`
	ErrorHash   string                  `json:"error_hash"`
	Done        bool                    `json:"done"`
	OutputRef   engineering.ArtifactRef `json:"output_ref"`
	ErrorRef    engineering.ArtifactRef `json:"error_ref"`
}

type RunHandle struct {
	RunID       string `json:"run_id"`
	Backend     string `json:"backend"`
	ContainerID string `json:"container_id"`
}

type Runner interface {
	Run(context.Context, Request) (Result, error)
	Start(context.Context, Request) (RunHandle, error)
	Observe(context.Context, string) (Result, error)
	Cancel(context.Context, string) error
}

type TerminalSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type TerminalSession interface {
	io.ReadWriteCloser
	Resize(context.Context, TerminalSize) error
	Wait(context.Context) (Result, error)
	Identity() RunHandle
}

type TerminalRunner interface {
	StartTerminal(context.Context, Request, TerminalSize) (TerminalSession, error)
}
