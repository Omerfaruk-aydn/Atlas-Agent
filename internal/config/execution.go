package config

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
)

// Execution configures command isolation independently of LSP and MCP.
type Execution struct {
	Mode            string   `json:"mode,omitempty" jsonschema:"enum=legacy,enum=container-required,default=legacy"`
	RuntimePath     string   `json:"runtime_path,omitempty"`
	Image           string   `json:"image,omitempty"`
	Network         string   `json:"network,omitempty" jsonschema:"enum=none,enum=unrestricted,default=none"`
	ReadOnly        bool     `json:"read_only,omitempty"`
	CPUs            float64  `json:"cpus,omitempty"`
	MemoryBytes     int64    `json:"memory_bytes,omitempty"`
	MaxProcesses    int      `json:"max_processes,omitempty"`
	TimeoutMS       int64    `json:"timeout_ms,omitempty"`
	EnvironmentKeys []string `json:"environment_keys,omitempty"`
}

// ValidateExecution rejects unsupported policies before provider initialization.
func (c *Config) ValidateExecution() error {
	if c.Options == nil || c.Options.Execution == nil {
		return nil
	}
	p := c.Options.Execution
	if p.Mode == "" || p.Mode == "legacy" {
		return nil
	}
	if p.Mode != "container-required" {
		return fmt.Errorf("unsupported execution mode")
	}
	if !filepath.IsAbs(p.RuntimePath) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[a-f0-9]{64}$`).MatchString(p.Image) {
		return fmt.Errorf("required execution needs an absolute runtime and digest image")
	}
	if p.Network != "" && p.Network != "none" && p.Network != "unrestricted" {
		return fmt.Errorf("unsupported execution network policy")
	}
	if math.IsNaN(p.CPUs) || math.IsInf(p.CPUs, 0) || p.CPUs < 0 || p.CPUs > 64 || p.MemoryBytes < 0 || p.MemoryBytes > 64*1024*1024*1024 || p.MemoryBytes > 0 && p.MemoryBytes < 64*1024*1024 || p.MaxProcesses < 0 || p.MaxProcesses > 4096 || p.TimeoutMS < 0 || p.TimeoutMS > 600000 {
		return fmt.Errorf("invalid execution resource bounds")
	}
	if len(p.EnvironmentKeys) > 64 {
		return fmt.Errorf("execution environment allow-list exceeds limit")
	}
	for _, key := range p.EnvironmentKeys {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) {
			return fmt.Errorf("invalid execution environment key")
		}
	}
	return nil
}
