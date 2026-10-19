package tools

import (
	"context"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
)

// recordFileBefore records path's content before the current message
// changes it, so a rewind to an earlier point can restore it. A file the
// session has not touched gets a baseline: message-less when it existed
// before this change, tagged with messageID when the change creates it, so
// a rewind to before the message deletes it again. A file whose content
// moved on outside the session gets a message-less drift version, as edit
// and write record.
func recordFileBefore(ctx context.Context, files history.Service, sessionID, path, content string, existed bool, messageID string) error {
	latest, err := files.GetByPathAndSession(ctx, path, sessionID)
	if err != nil {
		baseline := ""
		if !existed {
			baseline = messageID
		}
		_, err = files.Create(ctx, sessionID, path, content, baseline)
		return err
	}
	if latest.Content != content {
		_, err = files.CreateVersion(ctx, sessionID, path, content, "")
	}
	return err
}

// recordFileAfter records path's content as produced by messageID, unless
// the latest recorded version already holds it.
func recordFileAfter(ctx context.Context, files history.Service, sessionID, path, content, messageID string) error {
	if latest, err := files.GetByPathAndSession(ctx, path, sessionID); err == nil && latest.Content == content && latest.MessageID != "" {
		return nil
	}
	_, err := files.CreateVersion(ctx, sessionID, path, content, messageID)
	return err
}

// historyPath is the absolute, cleaned path history records for target,
// which an edit plan may give relative to its root; rewind writes files
// back at the recorded path.
func historyPath(root, target string) string {
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	return filepath.Clean(target)
}
