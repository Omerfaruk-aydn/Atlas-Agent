package tools

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

// LocalAppToolName is the local app builder and manager tool.
const LocalAppToolName = "local_app"

//go:embed local_app.md
var localAppDescription string

// LocalAppParams are the local_app tool's inputs.
type LocalAppParams struct {
	Action      string          `json:"action" enum:"runtimes,plan,scaffold,verify,start,stop,status,list,logs,record_acceptance,backup,backups,restore,update_begin,update_finish,rollback,register,unregister" description:"Lifecycle step; see the description for the order."`
	AppID       string          `json:"app_id,omitempty" description:"The app id returned by plan or list."`
	Name        string          `json:"name,omitempty" description:"plan: short app name in the user's language."`
	Description string          `json:"description,omitempty" description:"plan: one sentence describing the app."`
	Spec        *localapps.Spec `json:"spec,omitempty" description:"plan: {purpose, flows[], entities[{name,label,fields[{name,label,type:text|longtext|integer|number|date|boolean|choice,required,choices[]}]}], acceptance[], language}. Names are lowercase identifiers; labels are in the user's language."`
	Root        string          `json:"root,omitempty" description:"plan: absolute empty folder (default: the user's atlas-apps folder). register: folder holding atlas-app.json."`
	Port        int             `json:"port,omitempty" description:"start: use this port instead of the manifest's for this run."`
	Lines       int             `json:"lines,omitempty" description:"logs: number of lines (default 80, max 400)."`
	Index       int             `json:"index,omitempty" description:"record_acceptance: 1-based acceptance criterion index."`
	Passed      bool            `json:"passed,omitempty" description:"record_acceptance: whether the criterion was observed to hold."`
	Method      string          `json:"method,omitempty" description:"record_acceptance: how it was checked: browser, http, scenario or manual."`
	Evidence    string          `json:"evidence,omitempty" description:"record_acceptance: what was observed (values read back, element texts)."`
	BackupID    string          `json:"backup_id,omitempty" description:"restore: id from backups."`
	WithSource  bool            `json:"with_source,omitempty" description:"backup: also archive the app's files."`
	Source      bool            `json:"source,omitempty" description:"restore: also restore the app's files (only for backups that include them)."`
	Reason      string          `json:"reason,omitempty" description:"backup: short label such as manual or before-import."`
	Task        string          `json:"task,omitempty" description:"update_begin: the requested change."`
}

type localAppTool struct {
	apps        *localapps.Manager
	permissions permission.Service
}

// NewLocalAppTool builds the local app lifecycle tool.
func NewLocalAppTool(apps *localapps.Manager, permissions permission.Service) fantasy.AgentTool {
	t := &localAppTool{apps: apps, permissions: permissions}
	return fantasy.NewAgentTool(LocalAppToolName, localAppDescription, func(ctx context.Context, _ LocalAppParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p LocalAppParams
		if err := strictDecode(call.Input, &p, `{"action":"list"}`); err != nil {
			return deviceErrorResponse(err), nil
		}
		resp, err := t.run(ctx, p, call)
		if err != nil {
			return localAppError(err), nil
		}
		return resp, nil
	})
}

// localAppError turns manager errors into the shared structured shape.
func localAppError(err error) fantasy.ToolResponse {
	var op *localapps.OpError
	if errors.As(err, &op) {
		de := deviceErr(errorCode(op.Code), "", op.Msg, op.Remedy)
		if len(op.LogTail) > 0 {
			de.Message += "\nLog:\n" + strings.Join(redactLines(op.LogTail), "\n")
		}
		return deviceErrorResponse(de)
	}
	return deviceErrorResponse(err)
}

func errorCode(err error) string {
	if err == nil {
		return "error"
	}
	return err.Error()
}

func (t *localAppTool) approve(ctx context.Context, call fantasy.ToolCall, action, desc string, params any, path string) error {
	if t.permissions == nil {
		return nil
	}
	ok, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: LocalAppToolName, Action: action, Description: desc, Params: params, Path: path})
	if err != nil {
		return err
	}
	if !ok {
		return deviceErr("permission_denied", "", "the user denied this action", "")
	}
	return nil
}

func (t *localAppTool) app(ctx context.Context, id string) (localapps.App, error) {
	if id == "" {
		return localapps.App{}, deviceErr("missing_field", "app_id", "app_id is required", `{"action":"list"}`)
	}
	return t.apps.Get(ctx, id)
}

func (t *localAppTool) run(ctx context.Context, p LocalAppParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	switch p.Action {
	case "runtimes":
		var out []map[string]any
		for _, rt := range localapps.SupportedRuntimes() {
			st := t.apps.CheckRuntime(ctx, rt.Kind)
			out = append(out, map[string]any{"runtime": rt, "status": st})
		}
		return deviceResponse(map[string]any{"ok": true, "runtimes": out, "apps_home": t.apps.AppsHome(), "note": "Only these runtimes can be built and managed. If none is available, tell the user what to install; do not build anything else as a managed app."}), nil
	case "list":
		apps, err := t.apps.List(ctx)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		rows := make([]map[string]any, 0, len(apps))
		for _, a := range apps {
			rows = append(rows, map[string]any{"id": a.ID, "name": a.Name, "state": a.State, "running": a.Running, "url": a.URL, "root": a.Root, "diagnostics": a.Diagnostics})
		}
		return deviceResponse(map[string]any{"ok": true, "apps": rows}), nil
	case "status":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		j, _ := t.apps.Journal(a.ID)
		return deviceResponse(map[string]any{"ok": true, "app": a, "last_operation": j}), nil
	case "plan":
		if p.Spec == nil {
			return fantasy.ToolResponse{}, deviceErr("missing_field", "spec", "plan needs the app definition", "")
		}
		rec, err := t.apps.Plan(ctx, localapps.PlanInput{Name: p.Name, Description: p.Description, Spec: *p.Spec, Root: p.Root})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "app_id": rec.ID, "root": rec.Root, "state": rec.State, "next": "scaffold"}), nil
	case "scaffold":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := t.approve(ctx, call, "scaffold", fmt.Sprintf("Create the app %q in %s (Python server, SQLite data, HTML interface, tests).", a.Name, a.Root), p, a.Root); err != nil {
			return fantasy.ToolResponse{}, err
		}
		res, err := t.apps.Scaffold(ctx, a.ID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "result": res, "next": "Adapt the files to the request with the edit tools (keep schema.json and migrations.py in step), then verify."}), nil
	case "verify", "update_finish":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := t.approve(ctx, call, p.Action, fmt.Sprintf("Build, test and run %q against a copy of its data to verify it.", a.Name), p, a.Root); err != nil {
			return fantasy.ToolResponse{}, err
		}
		if p.Action == "update_finish" {
			v, msg, err := t.apps.FinishUpdate(ctx, a.ID)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			return deviceResponse(map[string]any{"ok": v.Passed, "verification": v, "message": msg}), nil
		}
		v, err := t.apps.Verify(ctx, a.ID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		fresh, _ := t.apps.Get(ctx, a.ID)
		resp := map[string]any{"ok": v.Passed, "verification": v, "state": fresh.State, "pending_acceptance": fresh.PendingAcceptance}
		if v.Passed && len(fresh.PendingAcceptance) > 0 {
			resp["next"] = "start the app and check each pending acceptance criterion in the real interface, then record_acceptance with what you observed"
		}
		return deviceResponse(resp), nil
	case "start":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := t.approve(ctx, call, "start", fmt.Sprintf("Start %q on 127.0.0.1 with its real data.", a.Name), p, a.Root); err != nil {
			return fantasy.ToolResponse{}, err
		}
		res, err := t.apps.Start(ctx, a.ID, localapps.StartOptions{Port: p.Port})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "result": res}), nil
	case "stop":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		out, err := t.apps.Stop(ctx, a.ID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "outcome": out}), nil
	case "logs":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "log_file": t.apps.LogPath(a.ID), "lines": redactLines(t.apps.TailLog(a.ID, cmp.Or(p.Lines, 80)))}), nil
	case "record_acceptance":
		rec, err := t.apps.RecordAcceptance(ctx, p.AppID, p.Index, p.Passed, cmp.Or(p.Method, "manual"), p.Evidence)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "state": rec.State}), nil
	case "backup":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		info, err := t.apps.Backup(ctx, a.ID, cmp.Or(p.Reason, "manual"), p.WithSource)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "backup": map[string]any{"id": info.ID, "path": info.Path, "size": info.Size, "schema_versions": info.Meta.SchemaVersions, "includes_source": info.Meta.IncludesSource}}), nil
	case "backups":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		list, err := t.apps.Backups(a.ID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		rows := make([]map[string]any, 0, len(list))
		for _, b := range list {
			rows = append(rows, map[string]any{"id": b.ID, "created_at": b.Meta.CreatedAt, "reason": b.Meta.Reason, "includes_source": b.Meta.IncludesSource, "schema_versions": b.Meta.SchemaVersions})
		}
		return deviceResponse(map[string]any{"ok": true, "backups": rows}), nil
	case "restore", "rollback":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		what := "data"
		if p.Source || p.Action == "rollback" {
			what = "files and data"
		}
		if err := t.approve(ctx, call, p.Action, fmt.Sprintf("Replace the %s of %q from a backup. The current state is backed up first.", what, a.Name), p, a.Root); err != nil {
			return fantasy.ToolResponse{}, err
		}
		var msg string
		if p.Action == "rollback" {
			msg, err = t.apps.Rollback(ctx, a.ID)
		} else {
			if p.BackupID == "" {
				return fantasy.ToolResponse{}, deviceErr("missing_field", "backup_id", "restore needs a backup id", `{"action":"backups","app_id":"`+a.ID+`"}`)
			}
			msg, err = t.apps.Restore(ctx, a.ID, p.BackupID, localapps.RestoreOptions{Source: p.Source})
		}
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "message": msg}), nil
	case "update_begin":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		info, err := t.apps.BeginUpdate(ctx, a.ID, p.Task)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "backup_id": info.ID, "root": a.Root, "next": "Change the files; for data model changes append a migration in migrations.py and update schema.json. Then update_finish."}), nil
	case "register":
		if err := t.approve(ctx, call, "register", "Add the app in "+p.Root+" to Atlas's app list.", p, p.Root); err != nil {
			return fantasy.ToolResponse{}, err
		}
		rec, err := t.apps.Register(ctx, p.Root)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "app_id": rec.ID, "state": rec.State, "next": "verify"}), nil
	case "unregister":
		a, err := t.app(ctx, p.AppID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := t.approve(ctx, call, "unregister", fmt.Sprintf("Remove %q from Atlas's app list. Files, data and backups stay.", a.Name), p, a.Root); err != nil {
			return fantasy.ToolResponse{}, err
		}
		msg, err := t.apps.Unregister(ctx, a.ID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "message": msg}), nil
	}
	return fantasy.ToolResponse{}, deviceErr("invalid_action", "action", "unknown action "+p.Action, `{"action":"list"}`)
}
