package backend

import (
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
)

// AnswerQuestion submits answers for a question. The returned bool
// reports whether this call resolved the pending request (true) or
// found it already resolved by a previous caller (false).
//
// When the request names its batch, only that batch can be resolved, so
// an answer composed for a batch another client already settled can never
// land on the next one in the queue.
func (b *Backend) AnswerQuestion(workspaceID string, req proto.QuestionAnswer) (bool, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return false, err
	}

	responses := make([]question.Answer, len(req.Responses))
	for i, r := range req.Responses {
		responses[i] = question.Answer{
			QuestionID:  r.QuestionID,
			SelectedIDs: r.SelectedIDs,
			FillInText:  r.FillInText,
			Yes:         r.Yes,
			Notes:       r.Notes,
		}
	}

	if req.BatchRequestID != "" {
		return ws.Questions.AnswerRequest(req.BatchRequestID, responses), nil
	}
	return ws.Questions.Answer(responses), nil
}

// CancelQuestion cancels the pending question for a workspace.
// Returns true if a question was cancelled, false if none was
// pending. A non-empty batchID cancels exactly that batch.
func (b *Backend) CancelQuestion(workspaceID string, batchID ...string) (bool, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return false, err
	}
	if len(batchID) > 0 && batchID[0] != "" {
		return ws.Questions.CancelRequest(batchID[0]), nil
	}
	return ws.Questions.Cancel(), nil
}
