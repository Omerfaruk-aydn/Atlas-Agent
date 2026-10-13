package tools

import (
	"context"
	"strings"
	"sync/atomic"
)

type (
	userInstructionKey   struct{}
	userInstructionIDKey struct{}
)

var userInstructionSequence atomic.Uint64

// WithUserInstruction carries the accepted human request before prompt
// hooks, summaries or model-generated continuation text are added.
func WithUserInstruction(ctx context.Context, text string) context.Context {
	ctx = context.WithValue(ctx, sourceCredentialGrantKey{}, &sourceCredentialGrant{used: make(map[string]bool)})
	ctx = context.WithValue(ctx, userInstructionIDKey{}, userInstructionSequence.Add(1))
	return context.WithValue(ctx, userInstructionKey{}, text)
}

func localComputerRequested(ctx context.Context) bool {
	text, _ := ctx.Value(userInstructionKey{}).(string)
	text = strings.ToLower(strings.TrimSpace(text))
	for _, negative := range []string{"bu bilgisayarda değil", "bu bilgisayarı değil", "not this computer", "uzak bilgisayar", "remote computer", "bağlandığın bilgisayar"} {
		if strings.Contains(text, negative) {
			return false
		}
	}
	for _, prefix := range []string{"bu bilgisayarda ", "bu bilgisayarı ", "yerel bilgisayarda ", "yerel pc'de ", "on this computer ", "use this computer", "on the local computer "} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}
