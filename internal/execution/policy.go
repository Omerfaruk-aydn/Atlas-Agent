package execution

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	imageDigest    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[a-f0-9]{64}$`)
	environmentKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func defaults(p ExecutionPolicy) ExecutionPolicy {
	if p.Mode == "" {
		p.Mode = "legacy"
	}
	if p.Network == "" {
		p.Network = "none"
	}
	if p.CPUs == 0 {
		p.CPUs = 2
	}
	if p.MemoryBytes == 0 {
		p.MemoryBytes = 2 * 1024 * 1024 * 1024
	}
	if p.MaxProcesses == 0 {
		p.MaxProcesses = 128
	}
	if p.TimeoutMS == 0 {
		p.TimeoutMS = 600000
	}
	p.EnvironmentKeys = append([]string(nil), p.EnvironmentKeys...)
	return p
}

func ValidatePolicy(ctx context.Context, policy ExecutionPolicy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := defaults(policy)
	if p.Mode == "legacy" {
		return nil
	}
	if p.Mode != "container-required" {
		return fmt.Errorf("unsupported execution mode")
	}
	if !filepath.IsAbs(p.RuntimePath) {
		return fmt.Errorf("execution runtime must be an absolute executable path")
	}
	info, err := os.Lstat(p.RuntimePath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("configured execution runtime is unavailable")
	}
	if !imageDigest.MatchString(p.Image) {
		return fmt.Errorf("execution image must use an immutable sha256 digest")
	}
	if p.Network != "none" && p.Network != "unrestricted" {
		return fmt.Errorf("unsupported execution network policy")
	}
	if math.IsNaN(p.CPUs) || math.IsInf(p.CPUs, 0) || p.CPUs <= 0 || p.CPUs > 64 || p.MemoryBytes < 64*1024*1024 || p.MemoryBytes > 64*1024*1024*1024 || p.MaxProcesses < 1 || p.MaxProcesses > 4096 || p.TimeoutMS < 1 || p.TimeoutMS > 600000 {
		return fmt.Errorf("execution resource policy is outside supported bounds")
	}
	if len(p.EnvironmentKeys) > 64 {
		return fmt.Errorf("execution environment allow-list exceeds limit")
	}
	for _, key := range p.EnvironmentKeys {
		if !environmentKey.MatchString(key) {
			return fmt.Errorf("invalid execution environment key")
		}
	}
	return nil
}

func forwardedEnvironment(p ExecutionPolicy, values []string) []string {
	allow := map[string]bool{}
	for _, key := range p.EnvironmentKeys {
		allow[key] = true
	}
	result := []string{}
	for _, value := range values {
		key, _, ok := strings.Cut(value, "=")
		if ok && allow[key] {
			result = append(result, value)
		}
	}
	return result
}
