//go:build windows

package computer

import "context"

type pointerFeedback struct {
	ctx  context.Context
	emit func(string, int, int)
}

// BindPointerFeedback scopes input metadata to one tool operation.
func (b *windowsBackend) BindPointerFeedback(ctx context.Context, emit func(string, int, int)) func() {
	owner := &pointerFeedback{ctx: ctx, emit: emit}
	b.pointerFeedback.Store(owner)
	return func() { b.pointerFeedback.CompareAndSwap(owner, nil) }
}

// AimPointer gates input again after the synchronous presentation barrier.
func (b *windowsBackend) aimPointer(x, y int) error {
	return b.presentPointer("aim", x, y)
}

func (b *windowsBackend) presentPointer(kind string, x, y int) error {
	owner := b.pointerFeedback.Load()
	if owner != nil {
		if err := owner.ctx.Err(); err != nil {
			return err
		}
	}
	b.emitPointer(kind, x, y)
	if owner != nil {
		if err := owner.ctx.Err(); err != nil {
			return err
		}
		if b.pointerFeedback.Load() != owner {
			return context.Canceled
		}
	}
	return nil
}

func (b *windowsBackend) emitPointer(kind string, x, y int) {
	if owner := b.pointerFeedback.Load(); owner != nil && owner.emit != nil {
		if kind == "move" && b.pointerHeld.Load() {
			kind = "drag_move"
		}
		owner.emit(kind, x+getSystemMetrics(smXVirtualScreen), y+getSystemMetrics(smYVirtualScreen))
	}
}
