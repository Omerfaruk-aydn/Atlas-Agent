package browser

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

var browserInstances atomic.Uint64

func newBrowserInstanceID() string {
	return fmt.Sprintf("i%x-%d", time.Now().UnixNano(), browserInstances.Add(1))
}

// ExecutionIdentity is where a browser call ran: the browser connection,
// the selected tab and the document loaded in its top frame. Evidence is
// tied to it, so a reconnect, another tab or a navigation is recognizable
// afterwards.
type ExecutionIdentity struct {
	Instance string
	TabID    string
	Document string
}

// IdentitySession reports the execution identity without dispatching input.
type IdentitySession interface {
	ExecutionIdentity(ctx context.Context) ExecutionIdentity
}

var _ IdentitySession = (*chromedpSession)(nil)

// ExecutionIdentity reads the top frame's loader id with a short bound; a
// closed tab or lost browser leaves Document empty.
func (s *chromedpSession) ExecutionIdentity(parent context.Context) ExecutionIdentity {
	id := ExecutionIdentity{Instance: s.instance, TabID: s.ActiveTab()}
	if s.disconnected() || s.currentTabGone() {
		return id
	}
	ctx, cancel := context.WithTimeout(s.currentContext(), time.Second)
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	if tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false); err == nil {
		if frame := browserFrameInfo(tree, ""); frame != nil {
			id.Document, _ = frame["loaderId"].(string)
		}
	}
	return id
}
