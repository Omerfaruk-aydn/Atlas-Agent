package integrations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
)

// AppAdapter lists the local apps Atlas manages.
type AppAdapter struct {
	Apps *localapps.Manager
}

// Items implements Adapter. Running state is live; health and readiness
// come from the app manager's records.
func (a *AppAdapter) Items(ctx context.Context, fp Fingerprinter) []Item {
	if a.Apps == nil {
		return nil
	}
	apps, err := a.Apps.List(ctx)
	if err != nil {
		return []Item{{
			ID: "app:registry", Name: i18n.M("Local app registry").String(), Kind: KindApp, Status: StatusConnectionError,
			LastError: i18n.Raw(err.Error()), Remedy: i18n.M("Check or restore the registry file in %s.", a.Apps.StateDir()),
			Source: Source{Origin: "app registry", Scope: "user", Path: a.Apps.StateDir()}, Live: true,
		}}
	}
	out := make([]Item, 0, len(apps))
	for _, app := range apps {
		it := Item{
			ID:          "app:" + app.ID,
			Name:        app.Name,
			Kind:        KindApp,
			Description: i18n.Raw(app.Description),
			Source:      Source{Origin: "app registry", Scope: "user", Path: app.Root},
			Auth:        AuthNone,
			Actions:     []Action{ActionTest, ActionOpenApp},
			AppID:       app.ID,
			Live:        true,
			CheckNote:   i18n.M("Free: reads the manifest and, when the app runs, calls its local health endpoint."),
		}
		if app.Tech != "" {
			it.Capabilities = append(it.Capabilities, i18n.Raw(app.Tech))
		}
		if app.Spec != nil {
			for _, e := range app.Spec.Entities {
				it.Capabilities = append(it.Capabilities, i18n.M("stores %s", e.Label))
			}
		}
		appStatus(&it, app)
		it.fingerprint = fp.Fingerprint("app", app.ID, app.State, app.Running, app.ManifestHash, app.CurrentSourceHash, app.SourceChanged, app.URL, app.Process, app.UpdatedAt)
		out = append(out, it)
	}
	return out
}

func appStatus(it *Item, app localapps.App) {
	switch {
	case app.ManifestError != "" || len(app.Diagnostics) > 0 && app.Manifest == nil:
		it.Status = StatusConnectionError
		it.LastError = i18n.Raw(strings.TrimSpace(strings.Join(append([]string{app.ManifestError}, app.Diagnostics...), " ")))
		it.Remedy = i18n.M("Open the app in /app to see the problem.")
	case app.SourceChanged:
		it.Status = StatusUnchecked
		it.Detail = i18n.M("The app's source changed; verify it again in /app.")
	case app.Running:
		it.Status = StatusUnchecked
		it.Detail = i18n.M("Running at %s", app.URL)
	case app.State == localapps.StateReady || app.State == localapps.StateVerified:
		it.Status = StatusReady
		it.Detail = i18n.M("Stopped; verified")
		if app.State == localapps.StateVerified {
			it.Detail = i18n.M("Stopped; verified, acceptance checks pending")
		}
	case app.State == localapps.StateFailed || app.State == localapps.StateUpdateFailed:
		it.Status = StatusLimited
		it.LastError = i18n.Raw(app.LastError)
		it.Remedy = i18n.M("Open the app in /app; its last verification failed.")
	default:
		it.Status = StatusUnchecked
		it.Detail = i18n.M("Not verified yet (%s)", string(app.State))
	}
	if app.LastVerification != nil {
		it.CheckedAt = app.LastVerification.At
	}
}

// Check implements Adapter.
func (a *AppAdapter) Check(ctx context.Context, item Item) CheckResult {
	id := strings.TrimPrefix(item.ID, "app:")
	app, err := a.Apps.Get(ctx, id)
	if err != nil {
		return CheckResult{Status: StatusConnectionError, Err: i18n.Raw(err.Error())}
	}
	if app.Manifest == nil {
		return CheckResult{Status: StatusConnectionError, Err: i18n.Raw(strings.Join(app.Diagnostics, " ")), Remedy: i18n.M("Open the app in /app to see the problem.")}
	}
	if app.SourceChanged {
		return CheckResult{Status: StatusUnchecked, Detail: i18n.M("The app's source changed; verify it again in /app.")}
	}
	rt := a.Apps.CheckRuntime(ctx, app.Manifest.Runtime)
	if !rt.Available {
		return CheckResult{Status: StatusMissingDependency, Err: i18n.Raw(rt.Problem), Remedy: i18n.M(rt.Remedy)}
	}
	if !app.Running {
		if app.State == localapps.StateReady || app.State == localapps.StateVerified {
			return CheckResult{Status: StatusReady, Detail: i18n.M("Stopped; the manifest and runtime are in order.")}
		}
		return CheckResult{Status: StatusUnchecked, Detail: i18n.M("The app has not passed verification yet.")}
	}
	ok, detail, err := a.Apps.Health(ctx, id)
	switch {
	case err != nil:
		return CheckResult{Status: StatusConnectionError, Err: i18n.Raw(err.Error())}
	case !ok:
		return CheckResult{Status: StatusConnectionError, Err: i18n.M("Health check failed: %s", detail), Remedy: i18n.M("Read the app's log in /app.")}
	}
	return CheckResult{Status: StatusReady, Detail: i18n.M("Running at %s; health check passed.", app.URL)}
}

// Do implements Adapter.
func (a *AppAdapter) Do(_ context.Context, item Item, action Action) (i18n.Message, error) {
	if action == ActionOpenApp {
		return i18n.M("Open /app and choose %s.", item.Name), nil
	}
	return i18n.Message{}, ErrUnsupported
}

// CheckRuntime implements localapps.RuntimeChecker through the service, so
// the answer is recorded under the dependency item that /integrations
// shows.
func (s *Service) CheckRuntime(ctx context.Context, kind string) localapps.RuntimeStatus {
	rt, ok := localapps.RuntimeFor(kind)
	if !ok {
		return localapps.RuntimeStatus{Kind: kind, Problem: fmt.Sprintf("The runtime %q is not supported.", kind)}
	}
	item, err := s.Check(ctx, "dependency:"+kind)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return localapps.RuntimeStatus{Kind: kind, Problem: "Runtime checks are not available.", Remedy: rt.Remedy}
		}
		return localapps.RuntimeStatus{Kind: kind, Problem: err.Error(), Remedy: rt.Remedy}
	}
	out := localapps.RuntimeStatus{Kind: kind, Available: item.Status == StatusReady, Remedy: rt.Remedy}
	if dep := s.dependencies(); dep != nil {
		if _, probe, ok := dep.Resolve(kind); ok {
			out.Executable, out.Version = probe.Path, probe.Version
		}
	}
	if !out.Available {
		out.Problem = item.LastError.String()
		if out.Problem == "" {
			out.Problem = rt.Name + " was not found."
		}
	}
	return out
}

func (s *Service) dependencies() *DependencyAdapter {
	for _, a := range s.adapterList() {
		if d, ok := a.(*DependencyAdapter); ok {
			return d
		}
	}
	return nil
}

// Runtimes converts the supported local app runtimes to dependency items.
func Runtimes() []Runtime {
	var out []Runtime
	for _, r := range localapps.SupportedRuntimes() {
		out = append(out, Runtime{
			Kind:        r.Kind,
			Name:        r.Name,
			Description: i18n.M("Runtime for local apps: %s.", r.Stack),
			Tool:        r.Tool,
			Constraint:  r.Constraint,
			Remedy:      i18n.M(r.Remedy),
		})
	}
	return out
}
