package engineering

import (
	"context"
	"fmt"
)

// HumanFindingConfirmation is supplied by a direct user control, never decoded
// from a model tool argument or a general permission grant.
type HumanFindingConfirmation func(context.Context, Finding, string) (bool, error)

func (s *Store) WaiveFinding(ctx context.Context, namespace, id, reason string, confirm HumanFindingConfirmation) (Record, error) {
	if !boundedText(reason, 2048) || confirm == nil {
		return Record{}, fmt.Errorf("explicit reason and direct human confirmation are required")
	}
	f, ref, err := s.ReadFinding(ctx, namespace, id)
	if err != nil {
		return Record{}, err
	}
	if f.Status == "verified" || f.Status == "waived" {
		return Record{}, fmt.Errorf("finding is already closed")
	}
	source, err := SourceFingerprint(ctx, f.Root, s.Dir())
	if err != nil {
		return Record{}, err
	}
	ok, err := confirm(ctx, f, reason)
	if err != nil {
		return Record{}, err
	}
	if !ok {
		return Record{}, fmt.Errorf("user declined the finding waiver")
	}
	after, err := SourceFingerprint(ctx, f.Root, s.Dir())
	if err != nil {
		return Record{}, err
	}
	if source != after {
		return Record{}, fmt.Errorf("source changed while confirming the waiver")
	}
	f.Status, f.WaiverReason, f.WaiverSourceFingerprint, f.WaiverProvenance = "waived", reason, source, "interactive-user"
	return s.saveFinding(ctx, namespace, f, ref.Revision, true)
}
