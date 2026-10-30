package modelrouter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyPrompt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		prompt string
		want   Tier
	}{
		{"find where the config is loaded", TierFast},
		{"summarize this file", TierFast},
		{"rename the variable foo to bar", TierFast},
		{"debug why the build is failing", TierStrong},
		{"review the architecture of the agent package", TierStrong},
		{"the failing tests in auth need a root cause", TierStrong},
		{"bu dosyayı özetle", TierFast},
		{"kullanıcıları listele ve göster", TierFast},
		{"güvenlik açığını incele ve mimariyi tasarla", TierStrong},
		{"başarısız test için hata ayıkla", TierStrong},
		{"hello there", ""},
		{"", ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, ClassifyPrompt(tc.prompt), tc.prompt)
	}
}

func TestStrongWinsOverCheapWording(t *testing.T) {
	t.Parallel()
	require.Equal(t, TierStrong, ClassifyPrompt("search the code and debug the deadlock"))
}

func TestStartsStrongAndKeepsItWithoutSignal(t *testing.T) {
	t.Parallel()
	r := New(Config{})
	d := r.Decide("s", Input{Prompt: "hello there"})
	require.Equal(t, TierStrong, d.Tier)
	require.False(t, d.Switched)
}

func TestCheapPromptDownshiftsAtTurnStart(t *testing.T) {
	t.Parallel()
	r := New(Config{})
	d := r.Decide("s", Input{Prompt: "list the files in internal/agent"})
	require.Equal(t, TierFast, d.Tier)
	require.True(t, d.Switched)
}

func TestErrorStreakEscalatesImmediately(t *testing.T) {
	t.Parallel()
	r := New(Config{})
	r.Decide("s", Input{Prompt: "show the readme"})
	failed := Step{Calls: []ToolCall{{Name: "edit", Failed: true}}}
	d := r.Decide("s", Input{StepNumber: 1, Steps: []Step{failed}})
	require.Equal(t, TierFast, d.Tier, "one failure is not a streak")
	d = r.Decide("s", Input{StepNumber: 2, Steps: []Step{failed, failed}})
	require.Equal(t, TierStrong, d.Tier)
	require.True(t, d.Switched)
	require.Contains(t, d.Reason, "failures")
}

func TestFailingTestOutputEscalates(t *testing.T) {
	t.Parallel()
	r := New(Config{})
	r.Decide("s", Input{Prompt: "show the readme"})
	step := Step{Calls: []ToolCall{{Name: "bash", Output: "--- FAIL: TestX (0.00s)\nFAIL"}}}
	d := r.Decide("s", Input{StepNumber: 1, Steps: []Step{step}})
	require.Equal(t, TierStrong, d.Tier)
}

func TestContextTooLargeForFastModel(t *testing.T) {
	t.Parallel()
	r := New(Config{})
	d := r.Decide("s", Input{Prompt: "list the files", ContextTokens: 90_000, FastWindow: 100_000})
	require.Equal(t, TierStrong, d.Tier)
	require.Contains(t, d.Reason, "context")
}

func TestNeedStrongOverridesPrompt(t *testing.T) {
	t.Parallel()
	r := New(Config{})
	d := r.Decide("s", Input{Prompt: "list the files", NeedStrong: true})
	require.Equal(t, TierStrong, d.Tier)
}

func readSteps(n int) []Step {
	steps := make([]Step, n)
	for i := range steps {
		steps[i] = Step{Calls: []ToolCall{{Name: "view"}}}
	}
	return steps
}

func TestDownshiftNeedsStreakAndDwell(t *testing.T) {
	t.Parallel()
	r := New(Config{MinDwell: 3, DownshiftStreak: 2})
	r.Decide("s", Input{Prompt: "hello there"})
	// Step 1: streak of one.
	d := r.Decide("s", Input{StepNumber: 1, Steps: readSteps(1)})
	require.Equal(t, TierStrong, d.Tier)
	// Step 2: streak of two but the dwell (started at the minimum) is
	// satisfied only once enough steps have passed since the last switch.
	d = r.Decide("s", Input{StepNumber: 2, Steps: readSteps(2)})
	require.Equal(t, TierFast, d.Tier)
	require.True(t, d.Switched)
}

func TestDownshiftRespectsDwellAfterSwitch(t *testing.T) {
	t.Parallel()
	r := New(Config{MinDwell: 4, DownshiftStreak: 2})
	r.Decide("s", Input{Prompt: "list files"}) // Fast.
	failed := Step{Calls: []ToolCall{{Name: "edit", Failed: true}}}
	d := r.Decide("s", Input{StepNumber: 1, Steps: []Step{failed, failed}})
	require.Equal(t, TierStrong, d.Tier)
	// Light steps right after the switch must not bounce back.
	d = r.Decide("s", Input{StepNumber: 2, Steps: readSteps(3)})
	require.Equal(t, TierStrong, d.Tier)
	d = r.Decide("s", Input{StepNumber: 3, Steps: readSteps(4)})
	require.Equal(t, TierStrong, d.Tier)
	d = r.Decide("s", Input{StepNumber: 4, Steps: readSteps(5)})
	require.Equal(t, TierStrong, d.Tier)
	d = r.Decide("s", Input{StepNumber: 5, Steps: readSteps(6)})
	require.Equal(t, TierFast, d.Tier)
}

func TestStrongTurnNeverDownshifts(t *testing.T) {
	t.Parallel()
	r := New(Config{MinDwell: 1, DownshiftStreak: 1})
	r.Decide("s", Input{Prompt: "debug the deadlock"})
	for i := 1; i < 8; i++ {
		d := r.Decide("s", Input{StepNumber: i, Steps: readSteps(i)})
		require.Equal(t, TierStrong, d.Tier)
	}
}

func TestNoCrossProviderDownshiftMidTurn(t *testing.T) {
	t.Parallel()
	r := New(Config{MinDwell: 1, DownshiftStreak: 1})
	r.Decide("s", Input{Prompt: "hello there"})
	d := r.Decide("s", Input{StepNumber: 1, Steps: readSteps(3), CrossProvider: true})
	require.Equal(t, TierStrong, d.Tier)
}

func TestDownshiftBlockedWhenCacheLossOutweighsSaving(t *testing.T) {
	t.Parallel()
	in := Input{
		ContextTokens: 100_000,
		Strong:        Prices{In: 3, Out: 15, CacheRead: 2.9, CacheWrite: 3.75},
		Fast:          Prices{In: 1, Out: 5, CacheRead: 0.9, CacheWrite: 1.25},
	}
	// 4 steps * 0.1M * 2.0 = 0.8 saved vs 0.1M * (1.25-0.9) = 0.035 primed.
	require.True(t, downshiftPaysOff(in))
	in.Fast = Prices{In: 10, Out: 5, CacheRead: 2.8, CacheWrite: 12}
	require.False(t, downshiftPaysOff(in))
	require.True(t, downshiftPaysOff(Input{}), "unknown prices never block")
}

func TestSessionsAreIndependent(t *testing.T) {
	t.Parallel()
	r := New(Config{})
	require.Equal(t, TierFast, r.Decide("a", Input{Prompt: "list files"}).Tier)
	require.Equal(t, TierStrong, r.Decide("b", Input{Prompt: "hello there"}).Tier)
	r.Forget("a")
	require.Equal(t, TierStrong, r.Decide("a", Input{Prompt: "hello there"}).Tier)
}

func TestStepCost(t *testing.T) {
	t.Parallel()
	p := Prices{In: 3, Out: 15, CacheRead: 0.3, CacheWrite: 3.75}
	require.InDelta(t, 3+15+0.3+3.75, StepCost(p, 1_000_000, 1_000_000, 1_000_000, 1_000_000), 1e-9)
}
