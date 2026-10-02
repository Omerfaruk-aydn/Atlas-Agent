package scenarios

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/stretchr/testify/require"
)

type fixtureBackend struct {
	store         *engineering.Store
	source        string
	change        bool
	kind, backend string
}

func (f *fixtureBackend) Source(context.Context) (string, error) { return f.source, nil }
func (f *fixtureBackend) ReadArtifact(ctx context.Context, ref engineering.ArtifactRef) ([]byte, error) {
	return f.store.ReadArtifact(ctx, ref)
}

func (f *fixtureBackend) Run(ctx context.Context, scenario Scenario) (ScenarioRun, error) {
	ref, err := f.store.PutArtifact(ctx, f.kind, []byte("observed fixture transcript"))
	if f.change {
		f.source = engineering.Hash("changed")
	}
	code := 0
	return ScenarioRun{Observed: true, Passed: true, Status: "passed", Artifacts: []engineering.ArtifactRef{ref}, Result: execution.Result{RunID: scenario.RunID, Backend: f.backend, Done: true, ExitCode: &code}}, err
}

func terminalFixture() Scenario {
	return Scenario{ID: "interaction", Version: 1, Target: "tui", Argv: []string{"fixture"}, Width: 80, Height: 24, Assertions: []Assertion{{Kind: "transcript", Expected: "hello"}}}
}

func TestScenarioLimitsAndSource(t *testing.T) {
	t.Parallel()
	scenario := terminalFixture()
	for _, count := range []int{64, 65} {
		scenario.Steps = make([]Step, count)
		for i := range scenario.Steps {
			scenario.Steps[i] = Step{Action: "input", Value: "x"}
		}
		if count == 64 {
			require.NoError(t, Validate(t.Context(), scenario))
		} else {
			require.Error(t, Validate(t.Context(), scenario))
		}
		scenario.Steps = nil
		scenario.Assertions = make([]Assertion, count)
		for i := range scenario.Assertions {
			scenario.Assertions[i] = Assertion{Kind: "transcript", Expected: "x"}
		}
		if count == 64 {
			require.NoError(t, Validate(t.Context(), scenario))
		} else {
			require.Error(t, Validate(t.Context(), scenario))
		}
	}
	f := &fixtureBackend{store: engineering.NewStore(t.TempDir()), source: engineering.Hash("source"), kind: "pty-transcript", backend: "pty-windows"}
	run, err := Run(t.Context(), terminalFixture(), f)
	require.NoError(t, err)
	require.True(t, run.Passed)
	require.NoError(t, Current(t.Context(), run, f))
	f.source = engineering.Hash("new source")
	require.ErrorContains(t, Current(t.Context(), run, f), "source changed")
	f.change = true
	run, err = Run(t.Context(), terminalFixture(), f)
	require.NoError(t, err)
	require.False(t, run.Passed)
	require.Equal(t, "stale", run.Status)
}

func TestScenarioUnavailableAndArtifactProvenance(t *testing.T) {
	t.Parallel()
	run, err := Run(t.Context(), terminalFixture(), nil)
	require.NoError(t, err)
	require.False(t, run.Passed)
	require.Equal(t, "unavailable", run.Status)
	for _, fixture := range []struct{ kind, backend string }{{"scenario-screenshot", "pty-windows"}, {"pty-transcript", "pipe"}} {
		f := &fixtureBackend{store: engineering.NewStore(t.TempDir()), source: engineering.Hash("source"), kind: fixture.kind, backend: fixture.backend}
		run, err := Run(t.Context(), terminalFixture(), f)
		require.NoError(t, err)
		require.False(t, run.Passed)
		require.Equal(t, "failed", run.Status)
	}
}

func TestScenarioStrictJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(terminalFixture())
	require.NoError(t, err)
	_, err = Parse(t.Context(), data)
	require.NoError(t, err)
	for _, invalid := range []string{strings.Replace(string(data), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(data), `"version":1`, `"Version":1`, 1), strings.TrimSuffix(string(data), "}") + `,"passed":true}`, string(data) + " {}"} {
		_, err := Parse(t.Context(), []byte(invalid))
		require.Error(t, err, invalid)
	}
}
