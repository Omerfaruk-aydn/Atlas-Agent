package permission

import "sync"

// maxDenyFeedback bounds one remembered comment.
const maxDenyFeedback = 4000

var denyFeedback sync.Map // tool call ID -> string

// SetDenyFeedback records the comment a user attached when declining the
// request made by a tool call ("request changes"). The tool reads it with
// TakeDenyFeedback and returns it to the model, so a refusal says what to
// change instead of just "no".
func SetDenyFeedback(toolCallID, text string) {
	if toolCallID == "" || text == "" {
		return
	}
	if r := []rune(text); len(r) > maxDenyFeedback {
		text = string(r[:maxDenyFeedback])
	}
	denyFeedback.Store(toolCallID, text)
}

// TakeDenyFeedback returns and forgets the comment recorded for a tool call.
func TakeDenyFeedback(toolCallID string) string {
	v, ok := denyFeedback.LoadAndDelete(toolCallID)
	if !ok {
		return ""
	}
	return v.(string)
}
