package tools

import "context"

// ImageRequest carries what a picture request needs beyond prompt and aspect.
// It rides on the context so every ImageGenerator keeps one signature.
type ImageRequest struct {
	// Text lets the picture contain words; the default asks for none,
	// which suits photographs and illustrations but not a painted screen.
	Text bool
	// References are pictures the model keeps the new one consistent with:
	// earlier screens of the same set.
	References []ImageRef
	// Quality is "high" for whole screens; empty keeps the provider default.
	Quality string
	// Designer lets the model work as the product designer: it may search the
	// web for the real subject and real apps before it draws, and it chooses
	// the layout itself from the brief instead of following a spec.
	Designer bool
}

// ImageRef is one reference picture.
type ImageRef struct {
	Data []byte
	Mime string
}

type imageRequestKey struct{}

// WithImageRequest attaches request options to a context.
func WithImageRequest(ctx context.Context, r ImageRequest) context.Context {
	return context.WithValue(ctx, imageRequestKey{}, r)
}

// ImageRequestFrom returns the options on a context, or the zero value.
func ImageRequestFrom(ctx context.Context) ImageRequest {
	r, _ := ctx.Value(imageRequestKey{}).(ImageRequest)
	return r
}
