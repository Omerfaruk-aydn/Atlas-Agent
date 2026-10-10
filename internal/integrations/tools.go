package integrations

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/environment"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

// ToolSpec describes a local executable Atlas uses.
type ToolSpec struct {
	ID          string
	Name        string
	Description i18n.Message
	// Candidates are executable names tried in order.
	Candidates []string
	// VersionArgs print the version; nil means presence only.
	VersionArgs []string
	// Find locates the tool when it is not on PATH (e.g. Chrome).
	Find func() string
}

// Probe is the observed state of one executable.
type Probe struct {
	Found   bool
	Path    string
	Version string
	Err     i18n.Message
	At      time.Time
}

// Prober runs version probes and shares their results between the tool and
// dependency views of the same executable.
type Prober struct {
	// LookPath and Run are replaceable in tests.
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, path string, args ...string) (string, error)

	mu    sync.Mutex
	cache map[string]Probe
	calls map[string]*probeCall
}

type probeCall struct {
	done chan struct{}
	res  Probe
}

// NewProber returns a prober using the real PATH.
func NewProber() *Prober {
	return &Prober{cache: map[string]Probe{}, calls: map[string]*probeCall{}}
}

func (p *Prober) lookPath(name string) (string, error) {
	if p.LookPath != nil {
		return p.LookPath(name)
	}
	return exec.LookPath(name)
}

func (p *Prober) run(ctx context.Context, path string, args ...string) (string, error) {
	if p.Run != nil {
		return p.Run(ctx, path, args...)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if len(out) > 8192 {
		out = out[:8192]
	}
	return string(out), err
}

// Cached returns the last probe of spec, if any.
func (p *Prober) Cached(id string) (Probe, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.cache[id]
	return r, ok
}

var versionPattern = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// Probe runs spec's probe; concurrent probes of the same tool share one run.
func (p *Prober) Probe(ctx context.Context, spec ToolSpec) Probe {
	p.mu.Lock()
	if c, ok := p.calls[spec.ID]; ok {
		p.mu.Unlock()
		select {
		case <-c.done:
			return c.res
		case <-ctx.Done():
			return Probe{Err: i18n.M("The version check did not finish in time."), At: time.Now()}
		}
	}
	c := &probeCall{done: make(chan struct{})}
	p.calls[spec.ID] = c
	p.mu.Unlock()

	res := p.probe(ctx, spec)

	p.mu.Lock()
	if ctx.Err() == nil {
		p.cache[spec.ID] = res
	}
	delete(p.calls, spec.ID)
	p.mu.Unlock()
	c.res = res
	close(c.done)
	return res
}

func (p *Prober) probe(ctx context.Context, spec ToolSpec) Probe {
	now := time.Now()
	var lastErr i18n.Message
	for _, name := range spec.Candidates {
		path, err := p.lookPath(name)
		if err != nil {
			continue
		}
		if spec.VersionArgs == nil {
			return Probe{Found: true, Path: path, At: now}
		}
		out, err := p.run(ctx, path, spec.VersionArgs...)
		if ctx.Err() != nil {
			return Probe{Err: i18n.M("The version check did not finish in time."), At: now}
		}
		version := versionPattern.FindString(out)
		if version == "" {
			lastErr = i18n.M("%s did not report a version.", path)
			if err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) {
					lastErr = i18n.M("%s exited with code %d without reporting a version.", path, exit.ExitCode())
				}
			}
			// On Windows a "python" that is only the Store installer alias
			// lands here; keep looking for a real interpreter.
			continue
		}
		return Probe{Found: true, Path: path, Version: version, At: now}
	}
	if spec.Find != nil {
		if path := spec.Find(); path != "" {
			return Probe{Found: true, Path: path, At: now}
		}
	}
	if lastErr.IsZero() {
		lastErr = i18n.M("Not found on PATH.")
	}
	return Probe{Err: lastErr, At: now}
}

// DefaultTools lists the executables Atlas's own tools use.
func DefaultTools(findChrome func() string) []ToolSpec {
	python := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		python = []string{"python", "python3"}
	}
	specs := []ToolSpec{
		{ID: "git", Name: "Git", Description: i18n.M("Version control; used by the git tools, worktrees and diffs."), Candidates: []string{"git"}, VersionArgs: []string{"--version"}},
		{ID: "gh", Name: "GitHub CLI", Description: i18n.M("Used to read pull requests and CI results."), Candidates: []string{"gh"}, VersionArgs: []string{"--version"}},
		{ID: "node", Name: "Node.js", Description: i18n.M("JavaScript runtime; used to run and test web projects."), Candidates: []string{"node"}, VersionArgs: []string{"--version"}},
		{ID: "npm", Name: "npm", Description: i18n.M("Node.js package manager."), Candidates: []string{"npm"}, VersionArgs: []string{"--version"}},
		{ID: "python", Name: "Python", Description: i18n.M("Python runtime; runs the local apps Atlas builds."), Candidates: python, VersionArgs: []string{"--version"}},
		{ID: "go", Name: "Go", Description: i18n.M("Go toolchain; used for Go projects."), Candidates: []string{"go"}, VersionArgs: []string{"version"}},
		{ID: "docker", Name: "Docker", Description: i18n.M("Container engine; used by the Docker MCP catalog and container tools."), Candidates: []string{"docker"}, VersionArgs: []string{"--version"}},
		{ID: "dlv", Name: "Delve", Description: i18n.M("Go debugger; used by the debugger tool."), Candidates: []string{"dlv"}, VersionArgs: []string{"version"}},
		{ID: "adb", Name: "Android Debug Bridge", Description: i18n.M("Used by the android tool to control devices and emulators."), Candidates: []string{"adb"}, VersionArgs: []string{"version"}},
		{ID: "ffmpeg", Name: "FFmpeg", Description: i18n.M("Media tool; used by the video tool."), Candidates: []string{"ffmpeg"}, VersionArgs: []string{"-version"}},
		{ID: "rg", Name: "ripgrep", Description: i18n.M("Fast search used by the grep tool when available."), Candidates: []string{"rg"}, VersionArgs: []string{"--version"}},
	}
	if findChrome != nil {
		specs = append(specs, ToolSpec{ID: "chrome", Name: "Chrome / Chromium", Description: i18n.M("Browser driven by the browser tool."), Find: findChrome})
	}
	return specs
}

// ToolAdapter lists local executables.
type ToolAdapter struct {
	Specs  []ToolSpec
	Prober *Prober
}

// Items implements Adapter. Status mirrors the shared probe cache.
func (a *ToolAdapter) Items(_ context.Context, fp Fingerprinter) []Item {
	out := make([]Item, 0, len(a.Specs))
	for _, spec := range a.Specs {
		it := Item{
			ID:          "tool:" + spec.ID,
			Name:        spec.Name,
			Kind:        KindTool,
			Description: spec.Description,
			Source:      Source{Origin: "PATH", Scope: "system"},
			Auth:        AuthNone,
			Status:      StatusUnchecked,
			Actions:     []Action{ActionTest},
			CheckNote:   i18n.M("Free: finds the program and asks it for its version."),
		}
		if probe, ok := a.Prober.Cached(spec.ID); ok {
			applyProbe(&it, probe, "")
			if it.Status != StatusReady {
				it.Remedy = installRemedy(spec.Name)
			}
		}
		it.fingerprint = fp.Fingerprint("tool", spec.ID)
		out = append(out, it)
	}
	return out
}

func applyProbe(it *Item, probe Probe, constraint string) {
	it.Live = true
	it.CheckedAt = probe.At
	if !probe.Found {
		it.Status = StatusMissingDependency
		it.LastError = probe.Err
		it.Detail = i18n.Message{}
		return
	}
	it.Source.Path = probe.Path
	it.Status = StatusReady
	it.LastError = i18n.Message{}
	switch {
	case probe.Version != "":
		it.Detail = i18n.M("Version %s at %s", probe.Version, probe.Path)
	default:
		it.Detail = i18n.M("Found at %s", probe.Path)
	}
	if constraint != "" {
		ok, err := environment.MatchesVersion(probe.Version, constraint)
		if err != nil || !ok {
			it.Status = StatusMissingDependency
			it.LastError = i18n.M("Version %s does not satisfy %s.", cmpOr(probe.Version, "unknown"), constraint)
		}
	}
}

// Check implements Adapter.
func (a *ToolAdapter) Check(ctx context.Context, item Item) CheckResult {
	for _, spec := range a.Specs {
		if "tool:"+spec.ID == item.ID {
			return probeResult(a.Prober.Probe(ctx, spec), "", installRemedy(spec.Name))
		}
	}
	return CheckResult{Status: StatusNotConfigured}
}

func installRemedy(name string) i18n.Message {
	return i18n.M("Install %s and make sure it is on PATH, then test again.", name)
}

func probeResult(probe Probe, constraint string, remedy i18n.Message) CheckResult {
	it := Item{}
	applyProbe(&it, probe, constraint)
	res := CheckResult{Status: it.Status, Detail: it.Detail, Err: it.LastError}
	if it.Status != StatusReady {
		res.Remedy = remedy
	}
	return res
}

// Do implements Adapter.
func (a *ToolAdapter) Do(context.Context, Item, Action) (i18n.Message, error) {
	return i18n.Message{}, ErrUnsupported
}

// Runtime is a language runtime local apps can be built on.
type Runtime struct {
	Kind        string
	Name        string
	Description i18n.Message
	Tool        string
	Constraint  string
	Remedy      i18n.Message
}

// DependencyAdapter lists app runtimes. Their state comes from the same
// probes as the matching tool, so the two can never disagree.
type DependencyAdapter struct {
	Runtimes []Runtime
	Tools    []ToolSpec
	Prober   *Prober
}

func (a *DependencyAdapter) spec(tool string) (ToolSpec, bool) {
	for _, s := range a.Tools {
		if s.ID == tool {
			return s, true
		}
	}
	return ToolSpec{}, false
}

// Items implements Adapter.
func (a *DependencyAdapter) Items(_ context.Context, fp Fingerprinter) []Item {
	out := make([]Item, 0, len(a.Runtimes))
	for _, rt := range a.Runtimes {
		it := Item{
			ID:           "dependency:" + rt.Kind,
			Name:         rt.Name,
			Kind:         KindDependency,
			Description:  rt.Description,
			Source:       Source{Origin: "PATH", Scope: "system"},
			Auth:         AuthNone,
			Status:       StatusUnchecked,
			Actions:      []Action{ActionTest},
			Capabilities: []i18n.Message{i18n.M("requires %s %s", rt.Tool, rt.Constraint)},
			CheckNote:    i18n.M("Free: shares the version check of the %s tool.", rt.Tool),
		}
		if probe, ok := a.Prober.Cached(rt.Tool); ok {
			applyProbe(&it, probe, rt.Constraint)
			if it.Status != StatusReady {
				it.Remedy = rt.Remedy
			}
		}
		it.fingerprint = fp.Fingerprint("dependency", rt.Kind, rt.Constraint)
		out = append(out, it)
	}
	return out
}

// Check implements Adapter.
func (a *DependencyAdapter) Check(ctx context.Context, item Item) CheckResult {
	for _, rt := range a.Runtimes {
		if "dependency:"+rt.Kind != item.ID {
			continue
		}
		spec, ok := a.spec(rt.Tool)
		if !ok {
			return CheckResult{Status: StatusMissingDependency, Err: i18n.M("Atlas does not know how to find %s.", rt.Tool)}
		}
		return probeResult(a.Prober.Probe(ctx, spec), rt.Constraint, rt.Remedy)
	}
	return CheckResult{Status: StatusNotConfigured}
}

// Do implements Adapter.
func (a *DependencyAdapter) Do(context.Context, Item, Action) (i18n.Message, error) {
	return i18n.Message{}, ErrUnsupported
}

// Resolve returns the probe behind runtime kind after a check.
func (a *DependencyAdapter) Resolve(kind string) (Runtime, Probe, bool) {
	for _, rt := range a.Runtimes {
		if rt.Kind == kind {
			probe, ok := a.Prober.Cached(rt.Tool)
			return rt, probe, ok
		}
	}
	return Runtime{}, Probe{}, false
}
