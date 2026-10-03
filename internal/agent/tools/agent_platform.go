package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

//go:embed agent_platform.md
var platformDescription string

type PlatformParams struct {
	Action         string   `json:"action"`
	ID             string   `json:"id,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	EverySeconds   int64    `json:"every_seconds,omitempty"`
	TimeoutSeconds int64    `json:"timeout_seconds,omitempty"`
	MaxRuns        int      `json:"max_runs,omitempty"`
	Title          string   `json:"title,omitempty"`
	Worker         string   `json:"worker,omitempty"`
	Attempt        string   `json:"attempt,omitempty"`
	Dependencies   []string `json:"dependencies,omitempty"`
	Acceptance     []string `json:"acceptance,omitempty"`
	Sources        []string `json:"sources,omitempty"`
	Text           string   `json:"text,omitempty"`
	Query          string   `json:"query,omitempty"`
	ValidUntil     int64    `json:"valid_until,omitempty"`
}

func NewPlatformTools(store *agentstate.Store, root, scope string, permissions permission.Service) []fantasy.AgentTool {
	out := []fantasy.AgentTool{}
	for _, name := range []string{"agent_jobs", "task_board", "source_memory"} {
		out = append(out, fantasy.NewAgentTool(name, platformDescription, func(ctx context.Context, p PlatformParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if store == nil {
				return fantasy.NewTextErrorResponse("Persistent agent state unavailable"), nil
			}
			sid := GetSessionFromContext(ctx)
			if sid == "" {
				return fantasy.ToolResponse{}, errors.New("session ID required")
			}
			ns := scope + "/" + name
			read := p.Action == "list" || p.Action == "search"
			if !read {
				if name == "task_board" && p.Action == "accept" {
					return fantasy.NewTextErrorResponse("Task acceptance is a user-owned CLI action; submit evidence for review."), nil
				}
				allowed, err := permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: sid, ToolCallID: call.ID, ToolName: name, Path: root, Action: p.Action, Description: "Update persistent " + name + " state", Params: p})
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				if !allowed {
					return NewPermissionDeniedResponse(permissions), nil
				}
			}
			var value any
			var err error
			switch name {
			case "agent_jobs":
				switch p.Action {
				case "list":
					value, err = agentstate.Jobs(ctx, store, ns)
				case "add":
					if ctx.Value(ScheduledTurnKey{}) == true {
						return fantasy.NewTextErrorResponse("Scheduled agents cannot create recurring jobs"), nil
					}
					timeout := p.TimeoutSeconds
					if timeout == 0 {
						timeout = 300
					}
					runs := p.MaxRuns
					if runs == 0 {
						runs = 10
					}
					j := agentstate.Job{ID: p.ID, Kind: p.Kind, Root: root, Prompt: p.Prompt, EverySeconds: p.EverySeconds, TimeoutSeconds: timeout, MaxRuns: runs, NextAt: time.Now().Unix() + p.EverySeconds}
					if p.Kind == "heartbeat" {
						j.SessionID = sid
					}
					err = agentstate.SaveJob(ctx, store, ns, j, 0)
					value = j
				default:
					err = agentstate.ControlJob(ctx, store, ns, p.ID, p.Action)
				}
			case "task_board":
				switch p.Action {
				case "list":
					value, err = agentstate.Tasks(ctx, store, ns)
				case "add":
					t := agentstate.Task{ID: p.ID, Title: p.Title, Root: root, Prompt: p.Prompt, Acceptance: p.Acceptance, Dependencies: p.Dependencies}
					err = agentstate.AddTask(ctx, store, ns, t)
					value = t
				case "claim":
					seconds := p.TimeoutSeconds
					if seconds == 0 {
						seconds = 300
					}
					value, err = agentstate.ClaimTask(ctx, store, ns, p.ID, p.Worker, seconds)
				case "submit":
					for _, path := range p.Sources {
						if _, e := agentstate.ReadSource(root, path); e != nil {
							return fantasy.NewTextErrorResponse(e.Error()), nil
						}
					}
					err = agentstate.TransitionTask(ctx, store, ns, p.ID, p.Action, p.Attempt, p.Sources, p.Text)
				default:
					err = agentstate.TransitionTask(ctx, store, ns, p.ID, p.Action, p.Attempt, p.Sources, p.Text)
				}
			case "source_memory":
				switch p.Action {
				case "list", "search":
					value, err = agentstate.Memories(ctx, store, ns, root, p.Query)
				case "add":
					m := agentstate.Memory{ID: p.ID, Text: p.Text, SessionID: sid, ValidUntil: p.ValidUntil}
					err = agentstate.SaveMemory(ctx, store, ns, root, m, p.Sources)
					value = m
				case "remove":
					err = agentstate.Update[agentstate.Memory](ctx, store, ns, p.ID, func(m *agentstate.Memory) error {
						if m.ID == "" {
							return errors.New("memory not found")
						}
						m.Superseded = true
						return nil
					})
				default:
					err = errors.New("memory action must be add, list, search or remove")
				}
			}
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if value == nil {
				value = map[string]string{"status": "updated", "id": p.ID}
			}
			data, err := json.Marshal(value)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if len(data) > 64*1024 {
				return fantasy.NewTextErrorResponse("Result exceeds 64 KiB; use narrower queries or the CLI inspection command"), nil
			}
			return fantasy.NewTextResponse(string(data)), nil
		}))
	}
	return out
}

type ScheduledTurnKey struct{}
