package voice

import (
	"context"
	"errors"
)

// CallLeg is one connected call as a transport presents it: a duplex audio
// stream plus control. The transport owns the connection; the gateway owns
// the conversation.
type CallLeg interface {
	// Encoding is the audio format of both directions.
	Encoding() Encoding
	// Audio delivers the caller's audio. It is closed when the stream ends.
	Audio() <-chan AudioFrame
	// Send queues audio for the caller.
	Send(ctx context.Context, frame AudioFrame) error
	// Clear drops audio queued but not yet played (barge-in).
	Clear(ctx context.Context) error
	// Mark asks to be told, through Marks, when playback reaches this
	// point.
	Mark(ctx context.Context, name string) error
	Marks() <-chan string
	// Hangup ends the call. It is idempotent.
	Hangup(ctx context.Context) error
	// Done is closed when the call is gone, whoever ended it.
	Done() <-chan struct{}
}

// DialRequest asks a transport to place an outbound call. The destination
// comes from the owner configuration, never from the model.
type DialRequest struct {
	CallID      string
	To          string
	RingTimeout int
	// StreamToken authenticates the media stream the answered call opens.
	StreamToken string
}

// Dialer places outbound calls.
type Dialer interface {
	Dial(ctx context.Context, req DialRequest) (providerCallID string, err error)
	// Cancel ends a call that is ringing or connected.
	Cancel(ctx context.Context, providerCallID string) error
}

// DialError reports a failed dial. NotPlaced is true only when the request
// provably never reached the provider (connection refused, DNS failure,
// a 4xx rejection), which is the only case a dial may be retried.
type DialError struct {
	NotPlaced bool
	Code      Code
	Err       error
}

func (e *DialError) Error() string { return "dial failed: " + string(e.Code) + ": " + e.Err.Error() }

func (e *DialError) Unwrap() error { return e.Err }

// ProvablyNotPlaced reports whether err says the call was never placed.
func ProvablyNotPlaced(err error) bool {
	var de *DialError
	return errors.As(err, &de) && de.NotPlaced
}

// CallStatus is a transport-neutral call progress report.
type CallStatus string

// Call statuses reported by transports.
const (
	StatusQueued     CallStatus = "queued"
	StatusRinging    CallStatus = "ringing"
	StatusInProgress CallStatus = "in-progress"
	StatusCompleted  CallStatus = "completed"
	StatusBusy       CallStatus = "busy"
	StatusNoAnswer   CallStatus = "no-answer"
	StatusFailed     CallStatus = "failed"
	StatusCanceled   CallStatus = "canceled"
	// StatusMachine marks an answer the transport attributes to a
	// machine.
	StatusMachine CallStatus = "machine"
)

// Notifier delivers a written message to the owner on one channel.
type Notifier interface {
	Channel() string
	Notify(ctx context.Context, text string) error
}
