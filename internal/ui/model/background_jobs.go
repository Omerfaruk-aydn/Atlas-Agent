package model

import (
	"context"
	"fmt"
	"image/color"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

// jobStatesTTL bounds how long the memoized background-jobs state may go
// without a re-probe being scheduled; job events normally refresh it much
// sooner. The backstop covers events missed across SSE reconnects in
// client/server mode — mirrors lspStatesTTL. Package var so tests can pin
// it.
var jobStatesTTL = 5 * time.Second

// jobStatesMsg delivers background jobs and sub-agent runs fetched
// off-thread.
type jobStatesMsg struct {
	jobs      []shell.BackgroundShellInfo
	subAgents []workspace.SubAgentRunInfo
}

// requestJobsRefresh schedules an off-thread refresh of the memoized jobs
// state. While a fetch is already in flight it only marks the state dirty;
// applyJobStates re-dispatches so the freshest data still lands.
func (m *UI) requestJobsRefresh() tea.Cmd {
	if m.jobsFetchInFlight {
		m.jobsRefreshQueued = true
		return nil
	}
	return m.dispatchJobsRefresh()
}

// dispatchJobsRefresh returns a command that fetches background jobs and
// the current session's sub-agent runs off the Update goroutine (each a
// synchronous HTTP round-trip in client/server mode), delivering a
// jobStatesMsg. It returns nil while a fetch is already in flight. The
// closure captures only locals (never m) so it is safe off-thread.
func (m *UI) dispatchJobsRefresh() tea.Cmd {
	if m.jobsFetchInFlight || m.com == nil || m.com.Workspace == nil {
		return nil
	}
	m.jobsFetchInFlight = true
	// Stamp the check time at dispatch too so the TTL backstop doesn't
	// keep re-requesting while this fetch is in flight.
	m.jobsCheckedAt = time.Now()
	ws := m.com.Workspace
	sessionID := ""
	if m.session != nil {
		sessionID = m.session.ID
	}
	return func() tea.Msg {
		jobs := ws.BackgroundJobsList()
		var subAgents []workspace.SubAgentRunInfo
		if sessionID != "" {
			subAgents = ws.SubAgentRunsList(context.Background(), sessionID)
		}
		return jobStatesMsg{jobs: jobs, subAgents: subAgents}
	}
}

// applyJobStates stores an off-thread jobs fetch result and re-dispatches
// when events arrived while it was in flight. Runs on the Update goroutine.
func (m *UI) applyJobStates(msg jobStatesMsg) tea.Cmd {
	m.jobsFetchInFlight = false
	m.jobsCheckedAt = time.Now()
	m.jobStates = msg.jobs
	m.subAgentRuns = msg.subAgents

	active := m.jobsHighlight.sync(m.jobsHighlightKeys())

	var cmds []tea.Cmd
	if m.jobsRefreshQueued {
		m.jobsRefreshQueued = false
		cmds = append(cmds, m.dispatchJobsRefresh())
	}
	if active {
		cmds = append(cmds, highlightTickCmd())
	}
	return tea.Batch(cmds...)
}

// jobsHighlightKeys returns the set of keys (running job IDs and sub-agent
// session IDs) currently shown in the Jobs section, for reconciling
// m.jobsHighlight.
func (m *UI) jobsHighlightKeys() map[string]struct{} {
	keys := make(map[string]struct{}, len(m.jobStates)+len(m.subAgentRuns))
	for _, j := range m.jobStates {
		if !j.Done {
			keys[j.ID] = struct{}{}
		}
	}
	for _, s := range m.subAgentRuns {
		keys[s.SessionID] = struct{}{}
	}
	return keys
}

// runningJobsCount returns the number of background jobs still running,
// shown as a compact indicator elsewhere in the UI.
func (m *UI) runningJobsCount() int {
	count := 0
	for _, j := range m.jobStates {
		if !j.Done {
			count++
		}
	}
	return count + len(m.subAgentRuns)
}

// jobsInfo renders the background jobs section: running shell jobs and
// in-flight sub-agent runs for the current session. Like lspInfo, it
// renders from memoized state only — see requestJobsRefresh.
func (m *UI) jobsInfo(width, maxItems int, isSection bool) string {
	t := m.com.Styles

	title := t.Resource.Heading.Render(m.com.Text("Jobs"))
	if isSection {
		title = common.Section(t, title, width)
	}

	var running []shell.BackgroundShellInfo
	for _, j := range m.jobStates {
		if !j.Done {
			running = append(running, j)
		}
	}

	if len(running) == 0 && len(m.subAgentRuns) == 0 {
		list := t.Resource.AdditionalText.Render(m.com.Text("None"))
		return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
	}

	list := jobsList(t, running, m.subAgentRuns, width, maxItems, m.jobsHighlight.progress, m.com.Styles.Logo.FieldColor)
	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
}

// jobsList renders running background jobs and sub-agent runs, truncating
// to maxItems if needed. A row whose key is still fading in (per progress,
// see highlightTracker) gets its title tinted toward accent.
func jobsList(t *styles.Styles, jobs []shell.BackgroundShellInfo, subAgents []workspace.SubAgentRunInfo, width, maxItems int, progress func(key string) float64, accent color.Color) string {
	if maxItems <= 0 {
		return ""
	}
	var rendered []string
	for _, j := range jobs {
		title := j.Command
		if title == "" {
			title = j.ID
		}
		description := t.Resource.StatusText.Render(t.RelativeTime(j.StartedAt))
		rendered = append(rendered, common.Status(t, common.StatusOpts{
			Icon:        t.Resource.BusyIcon.String(),
			Title:       title,
			TitleColor:  common.BlendColor(t.Resource.DefaultTitleFg, accent, progress(j.ID)),
			Description: description,
		}, width))
	}
	for _, s := range subAgents {
		title := s.Title
		if title == "" {
			title = t.Text("Sub-agent")
		}
		description := t.Resource.StatusText.Render(t.RelativeTime(s.StartedAt))
		rendered = append(rendered, common.Status(t, common.StatusOpts{
			Icon:        t.Resource.BusyIcon.String(),
			Title:       title,
			TitleColor:  common.BlendColor(t.Resource.DefaultTitleFg, accent, progress(s.SessionID)),
			Description: description,
		}, width))
	}

	if len(rendered) > maxItems {
		visibleItems := rendered[:maxItems-1]
		remaining := len(rendered) - maxItems
		visibleItems = append(visibleItems, t.Resource.AdditionalText.Render(fmt.Sprintf(t.Text("…and %d more"), remaining)))
		return lipgloss.JoinVertical(lipgloss.Left, visibleItems...)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rendered...)
}
