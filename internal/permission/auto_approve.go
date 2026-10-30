package permission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ApprovalClass is the result of contextual auto-approval classification.
type ApprovalClass string

const (
	ApprovalSafe    ApprovalClass = "safe"
	ApprovalRisky   ApprovalClass = "risky"
	ApprovalUnknown ApprovalClass = "unknown"
)

// ApprovalDecision includes a human-readable explanation for audit and UI.
type ApprovalDecision struct {
	Class  ApprovalClass `json:"class"`
	Reason string        `json:"reason"`
	Source string        `json:"source"`
}

// AutoApprover classifies only requests that deterministic policy cannot
// decide. Implementations must fail closed by returning an error or unknown.
type AutoApprover interface {
	ClassifyPermission(context.Context, CreatePermissionRequest) (ApprovalDecision, error)
}

// AutoApprovalService installs the optional contextual classifier after model
// initialization, which necessarily happens after the permission service.
type AutoApprovalService interface {
	SetAutoApprover(AutoApprover)
}

func (s *permissionService) SetAutoApprover(approver AutoApprover) {
	s.autoApprover = approver
}

var (
	riskyShellCommand   = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(sudo|doas|su|runas|rm|del|erase|rmdir|rd|unlink|shred|wipefs|remove-item|ri|format\s+[a-z]:|mkfs(\.[a-z0-9]+)?|diskpart|fdisk|parted|shutdown|reboot|poweroff|halt|stop-computer|restart-computer|git\s+(push|reset|clean|rebase)|git\s+branch\s+(-[^ ]*d|--delete)|curl|wget|invoke-webrequest|invoke-restmethod|iwr|irm|nc|ncat|netcat|scp|sftp|ftp|ssh|telnet|reg\s+(add|delete|import)|bcdedit|takeown|icacls|systemctl|service|docker\s+(rm|rmi|system\s+prune)|kubectl\s+(apply|delete|drain)|terraform\s+(apply|destroy)|npm\s+(install|uninstall|update)|pnpm\s+(add|remove|update)|yarn\s+(add|remove|upgrade)|pip(3)?\s+(install|uninstall)|go\s+(install|get))([^a-z0-9_]|$)`)
	riskyGitMutation    = regexp.MustCompile(`(?i)\bgit\b[^;&|]*\s(push|reset|clean|rebase)(\s|$)`)
	riskyShellExecution = regexp.MustCompile(`(?i)(\|\s*(sh|bash|zsh|ksh|pwsh|powershell)([^a-z0-9_]|$)|-enc(odedcommand)?\s|frombase64string|certutil\s+-decode|\bdd\s+[^;|&]*\bof=|\b(eval|exec)\s+(['"]|\$)|\b(python(3)?|perl|ruby|node)\s+-[ce]\s)`)
)

// riskyShellPath catches common credential and system-file reads without
// treating ordinary source-file inspection as dangerous.
var riskyShellPath = regexp.MustCompile(`(?i)(^|[\s/"'=])(\.env([\s./\\]|$)|id_rsa|id_ed25519|credentials(\.|$)|\.aws[\\/]|\.ssh[\\/]|etc[\\/]shadow|etc[\\/]passwd)`)

// ClassifyDeterministic applies conservative rules before any model is used.
// In particular, it never approves commands merely because a model says so.
// ClassifyBashCommand applies the same conservative command-risk rules used
// by auto-approval. It never returns safe for an unverified command.
func ClassifyBashCommand(command string) ApprovalDecision {
	return ClassifyDeterministic(CreatePermissionRequest{ToolName: "bash", Params: map[string]string{"command": command}}, "")
}

func ClassifyDeterministic(req CreatePermissionRequest, workingDir string) ApprovalDecision {
	if req.ToolName == "bash" {
		command := commandText(req.Params)
		if riskyShellCommand.MatchString(command) || riskyGitMutation.MatchString(command) || riskyShellExecution.MatchString(command) || riskyShellPath.MatchString(command) {
			return ApprovalDecision{Class: ApprovalRisky, Reason: "Destructive, privileged, sensitive, remote execution, or system-changing command.", Source: "deterministic"}
		}
		if req.Safe {
			return ApprovalDecision{Class: ApprovalSafe, Reason: "The tool verified a read-only shell operation.", Source: "deterministic"}
		}
		return ApprovalDecision{Class: ApprovalUnknown, Reason: "Shell commands require contextual classification.", Source: "deterministic"}
	}
	if req.Safe && CategoryForTool(req.ToolName) != CategoryNetwork {
		return ApprovalDecision{Class: ApprovalSafe, Reason: "The tool verified a read-only operation.", Source: "deterministic"}
	}
	if CategoryForTool(req.ToolName) == CategoryReadOnly && pathWithin(req.Path, workingDir) {
		return ApprovalDecision{Class: ApprovalSafe, Reason: "Read-only tool within the working directory.", Source: "deterministic"}
	}
	return ApprovalDecision{Class: ApprovalUnknown, Reason: "The operation is not covered by a safe deterministic rule.", Source: "deterministic"}
}

func commandText(params any) string {
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Sprint(params)
	}
	var fields struct {
		Command string   `json:"command"`
		Argv    []string `json:"argv"`
	}
	if json.Unmarshal(data, &fields) == nil {
		return strings.Join(append([]string{fields.Command}, fields.Argv...), " ")
	}
	return string(data)
}

func pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type permissionAudit struct {
	Timestamp  time.Time        `json:"timestamp"`
	SessionID  string           `json:"session_id"`
	ToolCallID string           `json:"tool_call_id"`
	ToolName   string           `json:"tool_name"`
	Action     string           `json:"action"`
	Decision   ApprovalDecision `json:"decision"`
}

func auditApproval(workingDir string, req CreatePermissionRequest, decision ApprovalDecision) {
	entry := permissionAudit{Timestamp: time.Now().UTC(), SessionID: req.SessionID, ToolCallID: req.ToolCallID, ToolName: req.ToolName, Action: req.Action, Decision: decision}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := filepath.Join(workingDir, ".atlas", "audit")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(path, "permissions.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(data, '\n'))
}

func classifyAutoApproval(ctx context.Context, approver AutoApprover, req CreatePermissionRequest, workingDir string) ApprovalDecision {
	decision := ClassifyDeterministic(req, workingDir)
	if decision.Class != ApprovalUnknown || approver == nil {
		return decision
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	decision, err := approver.ClassifyPermission(ctx, req)
	if err != nil && !errors.Is(err, context.Canceled) {
		return ApprovalDecision{Class: ApprovalUnknown, Reason: "Classifier failed; user confirmation is required.", Source: "fallback"}
	}
	decision.Class = normalizeApprovalClass(decision.Class)
	decision.Source = "model"
	if decision.Reason == "" {
		decision.Reason = "Contextual classifier returned no explanation."
	}
	return decision
}

func normalizeApprovalClass(class ApprovalClass) ApprovalClass {
	switch class {
	case ApprovalSafe, ApprovalRisky:
		return class
	default:
		return ApprovalUnknown
	}
}
