package agent

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// Long work is built feature by feature, and once a feature is finished and
// checked, almost everything said while building it is spent: the files it
// read, the output of every run, the attempts that failed. Carrying it into
// the next feature resends it on every step and brings a compaction sooner,
// and a compaction is a model's summary of the conversation, which can drop
// what matters. The work plan and the progress notes are the record of the
// work by design, so at that boundary the conversation is replaced by a
// handoff built from them, with no model call: the user's own messages,
// the progress notes, the files changed, the jobs still running and the
// next feature. The plan itself is shown again with every request.
//
// A handoff happens only when nothing can be lost: a feature was marked
// done during the turn, every check the contract requires passed after the
// last change, the progress notes were written after the plan changed, and
// the context is large enough for the saving to matter.

// featureHandoffAt is the share of the context window in use above which a
// finished feature hands off. Below it the conversation is cheap to keep.
const featureHandoffAt = 0.4

// handoff limits keep the handoff message a fraction of what it replaces.
const (
	handoffMaxTurnText = 4 << 10
	handoffMaxProgress = 8 << 10
	handoffMaxFiles    = 60
)

// handoffWatch follows one turn for a feature boundary.
type handoffWatch struct {
	// baseline holds the features done when the turn started; only a
	// feature finished during the turn hands off.
	baseline map[string]bool
	// scratch replays the turn's steps on a copy of the evaluator ledger,
	// so the contract can be checked mid-turn without touching the ledger
	// the completion gate reads at the end of the turn.
	scratch *evaluatorLedger
	seen    int
	// feature is the finished feature that triggered the handoff.
	feature workplan.Feature
}

