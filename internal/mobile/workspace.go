package mobile

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	xdraw "golang.org/x/image/draw"
)

// ErrRemoteWorkspace is returned in client/server mode, where questions and
// events live in another process.
var ErrRemoteWorkspace = errors.New("phone control needs Atlas running in local mode (not ATLAS_AGENT_CLIENT_SERVER)")

type appBackend struct {
	workspace.Workspace
	app *workspace.AppWorkspace
}

func (b appBackend) QuestionAnswerRequest(id string, answers []question.Answer) bool {
	return b.app.App().Questions.AnswerRequest(id, answers)
}

func (b appBackend) QuestionCancelRequest(id string) bool {
	return b.app.App().Questions.CancelRequest(id)
}

// FromWorkspace adapts the running local workspace: the backend, the event
// stream (the same one the TUI consumes) and a screen preview source.
func FromWorkspace(ws workspace.Workspace) (Backend, func(context.Context) <-chan any, func() ([]byte, error), error) {
	aw, ok := ws.(*workspace.AppWorkspace)
	if !ok || aw.App() == nil {
		return nil, nil, nil, ErrRemoteWorkspace
	}
	events := func(ctx context.Context) <-chan any {
		src := aw.App().Events(ctx)
		out := make(chan any, 256)
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-src:
					if !ok {
						return
					}
					select {
					case out <- any(ev.Payload):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return out
	}
	var preview func() ([]byte, error)
	if backend := computer.OpenOrNil(); backend != nil {
		preview = func() ([]byte, error) { return screenPreview(backend) }
	}
	return appBackend{Workspace: ws, app: aw}, events, preview, nil
}

// screenPreview captures the screen and returns a small JPEG: at most 800
// pixels wide at quality 55, to bound both bandwidth and detail.
func screenPreview(b computer.Backend) ([]byte, error) {
	data, err := b.Screenshot()
	if err != nil {
		return nil, err
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w > 800 {
		h = h * 800 / w
		w = 800
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 55}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
