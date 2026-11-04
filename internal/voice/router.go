package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

// Priorities.
const (
	PriorityNormal = "normal"
	PriorityUrgent = "urgent"
)

// Channels.
const (
	ChannelPSTN          = "pstn"
	ChannelWhatsAppVoice = "whatsapp_voice"
	ChannelSMS           = "sms"
	ChannelWhatsAppText  = "whatsapp"
	ChannelTelegram      = "telegram"
)

// Call results reported to the agent.
const (
	ResultAnswered    = "answered"
	ResultNoAnswer    = "no_answer"
	ResultRejected    = "rejected"
	ResultFailed      = "failed"
	ResultRateLimited = "rate_limited"
	ResultDuplicate   = "duplicate"
)

const maxReasonRunes = 500

// OwnerCallRequest asks to call the owner. There is no destination field:
// the number always comes from configuration.
type OwnerCallRequest struct {
	Reason           string `json:"reason"`
	Priority         string `json:"priority,omitempty"`
	PreferredChannel string `json:"preferred_channel,omitempty"`
	EventID          string `json:"event_id,omitempty"`
}

// OwnerCallResult is what the agent learns about an owner call.
type OwnerCallResult struct {
	CallID   string   `json:"call_id,omitempty"`
	Result   string   `json:"result"`
	Channel  string   `json:"channel"`
	EventID  string   `json:"event_id"`
	Attempts int      `json:"attempts"`
	Fallback []string `json:"fallback,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// RouterOptions configures a [Router].
type RouterOptions struct {
	Config    Config
	Gateway   *Gateway
	Dialer    Dialer
	Notifiers []Notifier
	Store     *Store
	Budget    *Budget
	Now       func() time.Time
	Env       func(string) string
	// RetryDelay is the pause before the single allowed retry. Defaults to
	// 20s.
	RetryDelay time.Duration
	// AnswerGrace is waited beyond the ring timeout for the transport's
	// final status. Defaults to 20s.
	AnswerGrace time.Duration
}

// Router places owner calls on the right channel and falls back to written
// messages when nobody answers.
type Router struct {
	opts      RouterOptions
	cfg       Config
	notifiers map[string]Notifier
}

// NewRouter builds a router.
func NewRouter(opts RouterOptions) *Router {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.RetryDelay == 0 {
		opts.RetryDelay = 20 * time.Second
	}
	if opts.AnswerGrace == 0 {
		opts.AnswerGrace = 20 * time.Second
	}
	r := &Router{opts: opts, cfg: opts.Config.WithDefaults(), notifiers: map[string]Notifier{}}
	for _, n := range opts.Notifiers {
		r.notifiers[n.Channel()] = n
	}
	return r
}

// DefaultEventID derives an idempotency key for a request without one, so
// the same reason within the same hour never rings twice.
func DefaultEventID(reason string, now time.Time) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(reason))))
	return "adhoc:" + hex.EncodeToString(h[:8]) + ":" + now.UTC().Format("2006010215")
}

// CallOwner calls the owner once per event id.
func (r *Router) CallOwner(ctx context.Context, req OwnerCallRequest) (OwnerCallResult, error) {
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		return OwnerCallResult{}, Errorf(CodeInvalidInput, "call owner", errors.New("reason is required"))
	}
	if utf8.RuneCountInString(req.Reason) > maxReasonRunes {
		return OwnerCallResult{}, Errorf(CodeInvalidInput, "call owner", fmt.Errorf("reason is longer than %d characters", maxReasonRunes))
	}
	switch req.Priority {
	case "":
		req.Priority = PriorityNormal
	case PriorityNormal, PriorityUrgent:
	default:
		return OwnerCallResult{}, Errorf(CodeInvalidInput, "call owner", errors.New("priority must be normal or urgent"))
	}
	if len(req.EventID) > 200 {
		return OwnerCallResult{}, Errorf(CodeInvalidInput, "call owner", errors.New("event_id is too long"))
	}
	if req.EventID == "" {
		req.EventID = DefaultEventID(req.Reason, r.opts.Now())
	}
	res := OwnerCallResult{EventID: req.EventID, Channel: ChannelPSTN}
	switch req.PreferredChannel {
	case "", ChannelPSTN:
	case ChannelWhatsAppVoice:
		// Business-initiated WhatsApp calls are not available in every
		// country (including Turkey); the phone network is the reliable
		// route.
		res.Note = "Outbound WhatsApp calls are not supported; the call goes over the phone network."
	default:
		return OwnerCallResult{}, Errorf(CodeInvalidInput, "call owner", errors.New("preferred_channel must be pstn or whatsapp_voice"))
	}
	if !r.cfg.Enabled {
		return res, ErrServiceDisabled
	}
	owner, err := r.cfg.OwnerNumber(r.opts.Env)
	if err != nil || owner == "" {
		return res, Errorf(CodeNotConfigured, "call owner", errors.New("owner phone number is not configured"))
	}

	placeholder := newID("evt_")
	prev, claimed, err := r.opts.Store.ClaimEvent(req.EventID, placeholder)
	if err != nil {
		return res, err
	}
	if !claimed {
		res.Result, res.CallID = ResultDuplicate, prev
		if rec, ok := r.opts.Store.Get(prev); ok {
			res.Note = fmt.Sprintf("This event was already handled; that call ended as %s. No new call was placed.", rec.State)
		} else {
			res.Note = "This event was already handled. No new call was placed."
		}
		return res, nil
	}
	if err := r.opts.Budget.AllowOutbound(req.Priority); err != nil {
		_ = r.opts.Store.ReleaseEvent(req.EventID, placeholder)
		res.Result, res.Note = ResultRateLimited, err.Error()
		res.Fallback = r.fallback(ctx, req)
		return res, nil
	}

	maxAttempts := 1
	if req.Priority == PriorityUrgent && r.cfg.Outbound.AllowRetry {
		maxAttempts = 2
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res.Attempts = attempt
		outcome, callID, placed, err := r.attempt(ctx, req, owner)
		res.CallID = callID
		if attempt == 1 {
			// Re-point the claim at the real call.
			_ = r.opts.Store.ReleaseEvent(req.EventID, placeholder)
			if callID != "" {
				_, _, _ = r.opts.Store.ClaimEvent(req.EventID, callID)
			}
		}
		if err != nil && !placed {
			res.Result, res.Note = ResultFailed, "The call could not be placed."
			if !ProvablyNotPlaced(err) {
				// The provider may have placed it: never ring again blindly.
				res.Note = "The call request had an uncertain outcome; it was not retried to avoid calling twice."
				break
			}
			if attempt == maxAttempts || ctx.Err() != nil {
				break
			}
			continue
		}
		switch outcome.State {
		case StateActive, StateEnded:
			res.Result = ResultAnswered
			return res, nil
		case StateRejected:
			res.Result = ResultRejected
		case StateNoAnswer:
			res.Result = ResultNoAnswer
		default:
			res.Result = ResultFailed
		}
		if res.Result != ResultNoAnswer || attempt == maxAttempts {
			break
		}
		if err := r.opts.Budget.AllowOutbound(PriorityUrgent); err != nil {
			break
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(r.opts.RetryDelay):
		}
	}
	if res.Result != ResultAnswered {
		res.Fallback = r.fallback(ctx, req)
		if res.CallID != "" {
			if _, err := r.opts.Store.Update(res.CallID, func(rec *CallRecord) { rec.Fallback = res.Fallback }); err != nil {
				slog.Warn("Failed to record call fallback", "call_id", res.CallID, "error", err)
			}
		}
		if len(res.Fallback) == 0 && res.Note == "" {
			res.Note = "No written fallback channel is configured; consider telling the owner another way."
		}
	}
	return res, nil
}

// attempt dials once and waits for the call to be answered or to fail.
func (r *Router) attempt(ctx context.Context, req OwnerCallRequest, owner string) (CallOutcome, string, bool, error) {
	callID, token, outcome, err := r.opts.Gateway.PrepareOutbound(OutboundRequest{EventID: req.EventID, Reason: req.Reason, Priority: req.Priority})
	if err != nil {
		return CallOutcome{}, "", false, err
	}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	pid, err := r.opts.Dialer.Dial(dctx, DialRequest{CallID: callID, To: owner, RingTimeout: r.cfg.Outbound.RingTimeoutSeconds, StreamToken: token})
	cancel()
	if err != nil {
		code := CodeTransportFailed
		var de *DialError
		if errors.As(err, &de) {
			code = de.Code
		}
		r.opts.Gateway.AbortOutbound(callID, code)
		slog.Warn("Failed to place owner call", "call_id", callID, "code", code, "not_placed", ProvablyNotPlaced(err))
		return CallOutcome{}, callID, false, err
	}
	r.opts.Gateway.BindProvider(callID, pid)
	slog.Info("Placed owner call", "call_id", callID, "priority", req.Priority, "to", MaskPhone(owner))

	wait := time.Duration(r.cfg.Outbound.RingTimeoutSeconds)*time.Second + r.opts.AnswerGrace
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case o := <-outcome:
		return o, callID, true, nil
	case <-timer.C:
		r.opts.Gateway.Cancel(callID)
		cctx, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_ = r.opts.Dialer.Cancel(cctx, pid)
		stop()
		return CallOutcome{State: StateNoAnswer, Code: CodeTimeout}, callID, true, nil
	case <-ctx.Done():
		// The tool call was abandoned, but the phone may already be
		// ringing; leave the call to finish on its own.
		return CallOutcome{State: StateFailed, Code: CodeCancelled}, callID, true, ctx.Err()
	}
}

// fallback sends the reason over the configured written channels and
// returns the channels that accepted it.
func (r *Router) fallback(ctx context.Context, req OwnerCallRequest) []string {
	var sent []string
	text := "Atlas: " + req.Reason
	for _, ch := range r.cfg.Outbound.Fallback {
		n, ok := r.notifiers[ch]
		if !ok {
			continue
		}
		nctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := n.Notify(nctx, text)
		cancel()
		if err != nil {
			slog.Warn("Written fallback failed", "channel", ch, "error", err)
			continue
		}
		sent = append(sent, ch)
		// One delivered written message is enough.
		break
	}
	return sent
}
