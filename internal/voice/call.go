package voice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	openingTurn = "t0"
	closingTurn = "bye"
	// duplicateWindow drops a final transcript identical to the previous
	// one when it arrives this soon after it.
	duplicateWindow = 2 * time.Second
	cleanupTimeout  = 10 * time.Second
	goodbyeTimeout  = 6 * time.Second
)

var errStale = errors.New("audio belongs to an abandoned turn")

// call is one connected conversation.
type call struct {
	g       *Gateway
	id      string
	rec     CallRecord
	leg     CallLeg
	machine *Machine
	limit   time.Duration
	opening string
	context string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	sttMu  sync.Mutex
	stt    RecognitionStream
	sttEnc Encoding
	ttsEnc Encoding

	speakMu sync.Mutex

	mu           sync.Mutex
	sessionID    string
	turnSeq      int
	currentTurn  string
	turnCancel   context.CancelFunc
	turnRunning  bool
	speakCancel  context.CancelFunc
	speakingTurn string
	confirm      *confirmWait
	lastFinal    string
	lastFinalAt  time.Time
	lastActivity time.Time
	connectedAt  time.Time
	metrics      CallMetrics
	turns        []TurnRecord
	ttsFailures  int
	endState     State
	endCode      Code
	ending       bool
}

type confirmWait struct {
	turnID string
	answer chan string
}

func (g *Gateway) newCall(p *pendingCall, leg CallLeg, limit time.Duration) *call {
	ctx, cancel := context.WithCancel(g.baseCtx)
	now := g.opts.Now()
	return &call{
		g: g, id: p.rec.ID, rec: p.rec, leg: leg, machine: p.machine, limit: limit,
		opening: p.opening, context: p.context, ctx: ctx, cancel: cancel,
		currentTurn: openingTurn, lastActivity: now, connectedAt: now,
	}
}

func (c *call) since() int64 { return c.g.opts.Now().Sub(c.connectedAt).Milliseconds() }

// finish records why the call ends and cancels everything in it. Only the
// first reason sticks.
func (c *call) finish(state State, code Code) {
	c.mu.Lock()
	if c.endState == "" {
		c.endState, c.endCode = state, code
	}
	c.mu.Unlock()
	c.cancel()
}

// run drives the call until it ends, then cleans up.
func (c *call) run(parent context.Context) {
	defer c.cleanup()
	m := c.machine
	_ = m.Transition(StateConnecting)
	if err := m.Transition(StateActive); err != nil {
		c.finish(StateFailed, CodeTransportFailed)
		return
	}
	if _, err := c.g.opts.Store.Update(c.id, func(r *CallRecord) {
		r.AnsweredAt = c.connectedAt
		r.State = m.State()
	}); err != nil {
		slog.Warn("Failed to record answered call", "call_id", c.id, "error", err)
	}

	var ok bool
	c.ttsEnc, _ = pickEncoding(c.g.opts.Synthesizer.OutputEncodings(), c.leg.Encoding())
	c.sttEnc, ok = pickEncoding(c.g.opts.Recognizer.InputEncodings(), c.leg.Encoding())
	if !ok {
		slog.Debug("Converting call audio for recognition", "call_id", c.id, "from", c.leg.Encoding(), "to", c.sttEnc)
	}

	sid, err := c.g.opts.Runner.NewSession(c.ctx, "Voice call "+c.connectedAt.In(c.g.cfg.Location()).Format("2006-01-02 15:04"))
	if err != nil {
		slog.Error("Failed to create voice session", "call_id", c.id, "error", err)
		c.sayAndEnd(c.g.phrases.AgentError, StateFailed, CodeAgentFailed)
		<-c.ctx.Done()
		return
	}
	c.mu.Lock()
	c.sessionID = sid
	c.mu.Unlock()

	stream, err := c.openRecognition()
	if err != nil {
		slog.Error("Failed to start speech recognition", "call_id", c.id, "code", CodeOf(err))
		c.sayAndEnd(c.g.phrases.ProviderError, StateFailed, CodeOf(err))
		<-c.ctx.Done()
		return
	}
	c.sttMu.Lock()
	c.stt = stream
	c.sttMu.Unlock()

	c.wg.Add(4)
	go c.pumpAudio()
	go c.recognize()
	go c.watchMarks()
	go c.watchdog(parent)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := c.speak(openingTurn, c.opening); err != nil && !errors.Is(err, errStale) && c.ctx.Err() == nil {
			slog.Error("Failed to speak call opening", "call_id", c.id, "code", CodeOf(err))
			c.finish(StateFailed, CodeOf(err))
		}
	}()
	<-c.ctx.Done()
}

func (c *call) openRecognition() (RecognitionStream, error) {
	return c.g.opts.Recognizer.StartRecognition(c.ctx, RecognitionConfig{
		CallID: c.id, Language: c.g.cfg.Language, Encoding: c.sttEnc, Keyterms: c.g.cfg.ElevenLabs.Keyterms,
	})
}

func (c *call) cleanup() {
	c.cancel()
	c.sttMu.Lock()
	if c.stt != nil {
		_ = c.stt.Close()
	}
	c.sttMu.Unlock()
	hctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	if err := c.leg.Hangup(hctx); err != nil {
		slog.Warn("Failed to hang up voice call", "call_id", c.id, "error", err)
	}
	stop()

	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(cleanupTimeout):
		slog.Error("Voice call goroutines did not stop in time", "call_id", c.id)
	}

	c.mu.Lock()
	// Turns still without an answer were cut off by the call ending.
	for i := range c.turns {
		if c.turns[i].Assistant == "" && c.turns[i].Code == "" {
			c.turns[i].Cancelled = true
		}
	}
	state, code := c.endState, c.endCode
	metrics, turns, sid := c.metrics, c.turns, c.sessionID
	c.mu.Unlock()
	final := c.machine.Finish(cmpState(state, StateEnded))
	keep := c.g.cfg.KeepTranscripts()
	if !keep {
		for i := range turns {
			turns[i].User, turns[i].Assistant = "", ""
		}
	}
	rec, err := c.g.opts.Store.Update(c.id, func(r *CallRecord) {
		r.State, r.Code, r.EndedAt = final, code, c.g.opts.Now()
		r.History, r.Metrics, r.Turns, r.SessionID = c.machine.History(), metrics, turns, sid
		r.Summary = summarize(turns, keep)
	})
	if err != nil {
		slog.Warn("Failed to record voice call", "call_id", c.id, "error", err)
	}
	slog.Info("Voice call ended", "call_id", c.id, "state", final, "code", code,
		"seconds", rec.Seconds(c.g.opts.Now()), "turns", metrics.Turns, "tool_calls", metrics.ToolCalls,
		"barge_ins", metrics.BargeIns, "stt_reconnects", metrics.STTReconnects,
		"first_transcript_ms", metrics.FirstTranscriptMS, "first_answer_ms", metrics.FirstAnswerMS, "first_audio_ms", metrics.FirstAudioMS)
}

func cmpState(a, b State) State {
	if a == "" {
		return b
	}
	return a
}

func summarize(turns []TurnRecord, keep bool) string {
	answered := 0
	for _, t := range turns {
		if t.Code == "" && !t.Cancelled {
			answered++
		}
	}
	s := fmt.Sprintf("%d turn(s), %d answered.", len(turns), answered)
	if keep && len(turns) > 0 {
		first := turns[0].User
		if utf8.RuneCountInString(first) > 160 {
			first = string([]rune(first)[:160]) + "…"
		}
		s += " First request: " + first
	}
	return s
}

// pumpAudio forwards caller audio to recognition.
func (c *call) pumpAudio() {
	defer c.wg.Done()
	audio := c.leg.Audio()
	for {
		select {
		case <-c.ctx.Done():
			return
		case f, ok := <-audio:
			if !ok {
				c.finish(StateEnded, "")
				return
			}
			if err := f.Validate(); err != nil {
				continue
			}
			c.mu.Lock()
			c.metrics.AudioInBytes += int64(len(f.Data))
			c.mu.Unlock()
			if f.Encoding != c.sttEnc {
				conv, err := Convert(f, c.sttEnc)
				if err != nil {
					continue
				}
				f = conv
			}
			c.sttMu.Lock()
			stream := c.stt
			c.sttMu.Unlock()
			if stream != nil {
				// A failed send means the stream is being replaced; the
				// recognize loop reconnects, and this frame is dropped.
				_ = stream.SendAudio(c.ctx, f)
			}
		}
	}
}

// recognize consumes recognition events, reopening the stream a bounded
// number of times when it drops.
func (c *call) recognize() {
	defer c.wg.Done()
	attempts := 0
	for {
		c.sttMu.Lock()
		stream := c.stt
		c.sttMu.Unlock()
		fatal := false
		for ev := range stream.Events() {
			if ev.Type == EventProviderError {
				slog.Warn("Speech recognition error", "call_id", c.id, "code", ev.Code, "retryable", ev.Retryable)
				if !ev.Retryable {
					fatal = true
				}
				_ = stream.Close()
				break
			}
			attempts = 0
			c.handleRecognition(ev)
		}
		if c.ctx.Err() != nil {
			return
		}
		attempts++
		if fatal || attempts > c.g.opts.MaxReconnects {
			c.sayAndEnd(c.g.phrases.ProviderError, StateFailed, CodeProviderUnavailable)
			return
		}
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(c.g.opts.ReconnectBackoff * time.Duration(1<<(attempts-1))):
		}
		next, err := c.openRecognition()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			slog.Warn("Failed to reopen speech recognition", "call_id", c.id, "code", CodeOf(err))
			if attempts >= c.g.opts.MaxReconnects {
				c.sayAndEnd(c.g.phrases.ProviderError, StateFailed, CodeOf(err))
				return
			}
			continue
		}
		c.sttMu.Lock()
		c.stt = next
		c.sttMu.Unlock()
		c.mu.Lock()
		c.metrics.STTReconnects++
		c.mu.Unlock()
	}
}

func (c *call) handleRecognition(ev Event) {
	switch ev.Type {
	case EventSpeechStarted:
		c.bargeIn()
	case EventTranscriptPartial:
		if strings.TrimSpace(ev.Text) != "" {
			c.bargeIn()
		}
	case EventTranscriptFinal:
		c.onFinal(ev.Text)
	}
}

// bargeIn stops playback the moment the caller starts talking.
func (c *call) bargeIn() {
	c.mu.Lock()
	c.lastActivity = c.g.opts.Now()
	cancel := c.speakCancel
	speaking := c.speakingTurn != "" && c.speakingTurn != closingTurn
	if speaking {
		c.speakingTurn = ""
		c.speakCancel = nil
		c.metrics.BargeIns++
	}
	c.mu.Unlock()
	if !speaking {
		return
	}
	if cancel != nil {
		cancel()
	}
	if err := c.leg.Clear(c.ctx); err != nil && c.ctx.Err() == nil {
		slog.Debug("Failed to clear call playback", "call_id", c.id, "error", err)
	}
	c.machine.TransitionFrom(StateListening, StateSpeaking)
}

func (c *call) onFinal(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if utf8.RuneCountInString(text) > maxUtteranceRunes {
		text = string([]rune(text)[:maxUtteranceRunes])
	}
	now := c.g.opts.Now()
	c.mu.Lock()
	if c.ending {
		c.mu.Unlock()
		return
	}
	key := strings.ToLower(text)
	if key == c.lastFinal && now.Sub(c.lastFinalAt) < duplicateWindow {
		c.mu.Unlock()
		return
	}
	c.lastFinal, c.lastFinalAt, c.lastActivity = key, now, now
	if c.metrics.FirstTranscriptMS == 0 {
		c.metrics.FirstTranscriptMS = max(c.since(), 1)
	}
	if w := c.confirm; w != nil {
		c.confirm = nil
		c.mu.Unlock()
		w.answer <- text
		return
	}
	// A new utterance supersedes the turn in flight: its result will not
	// be spoken and its tools are cancelled.
	if c.turnCancel != nil {
		c.turnCancel()
		c.markCancelledLocked(c.currentTurn)
	}
	c.turnSeq++
	turnID := fmt.Sprintf("t%d", c.turnSeq)
	runID := fmt.Sprintf("%s:%d", c.id, c.turnSeq)
	tctx, cancel := context.WithTimeout(c.ctx, time.Duration(c.g.cfg.TurnTimeoutSeconds)*time.Second)
	c.currentTurn, c.turnCancel, c.turnRunning = turnID, cancel, true
	c.metrics.Turns++
	c.turns = append(c.turns, TurnRecord{TurnID: turnID, RunID: runID, At: now, User: text})
	stopSpeaking := c.speakCancel
	c.speakCancel, c.speakingTurn = nil, ""
	c.mu.Unlock()
	if stopSpeaking != nil {
		stopSpeaking()
	}
	c.machine.TransitionFrom(StateToolRunning, StateActive, StateListening, StateSpeaking)
	c.wg.Add(1)
	go c.runTurn(tctx, cancel, turnID, runID, text)
}

func (c *call) markCancelledLocked(turnID string) {
	for i := range c.turns {
		if c.turns[i].TurnID == turnID && c.turns[i].Assistant == "" && c.turns[i].Code == "" {
			c.turns[i].Cancelled = true
		}
	}
}

func (c *call) isCurrent(turnID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentTurn == turnID && !c.ending
}

func (c *call) runTurn(ctx context.Context, cancel context.CancelFunc, turnID, runID, text string) {
	defer c.wg.Done()
	defer cancel()
	turn := &Turn{CallID: c.id, TurnID: turnID, Policy: c.g.opts.Policy, Confirm: c.confirmer(turnID), MaxSteps: c.g.cfg.MaxToolCallsPerTurn}
	c.mu.Lock()
	sid := c.sessionID
	c.mu.Unlock()

	// A slow turn gets one short holding phrase. The goroutine is counted
	// before the turn starts, so cleanup always waits for it.
	answered := make(chan struct{})
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		timer := time.NewTimer(c.g.opts.ThinkingDelay)
		defer timer.Stop()
		select {
		case <-answered:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		// turnRunning is cleared before the answer is spoken, so the
		// holding phrase can never follow the answer.
		c.mu.Lock()
		idle := c.currentTurn == turnID && c.turnRunning && c.speakingTurn == "" && c.confirm == nil && !c.ending
		c.mu.Unlock()
		if idle {
			_ = c.speak(turnID, c.g.phrases.Thinking)
		}
	}()
	res, err, _ := c.g.runs.Do(WithTurn(ctx, turn), runID, func(ctx context.Context) (TurnResult, error) {
		return c.g.opts.Runner.RunTurn(ctx, TurnRequest{
			CallID: c.id, SessionID: sid, TurnID: turnID, RunID: runID, Text: text,
			Prompt: c.g.turnPrompt(c.id, turnID, c.context, text),
		})
	})
	close(answered)

	c.mu.Lock()
	c.metrics.ToolCalls += turn.Steps()
	current := c.currentTurn == turnID && !c.ending
	if c.metrics.FirstAnswerMS == 0 && err == nil {
		c.metrics.FirstAnswerMS = max(c.since(), 1)
	}
	c.mu.Unlock()
	if !current {
		return
	}

	reply, code := "", Code("")
	switch {
	case err != nil && ctx.Err() != nil && c.ctx.Err() != nil:
		return
	case err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		reply, code = c.g.phrases.AgentError, CodeTimeout
	case err != nil:
		code = CodeOf(err)
		reply = c.g.phrases.AgentError
		if code == CodePermissionRequired {
			reply = c.g.phrases.Permission
		}
		slog.Warn("Voice turn failed", "call_id", c.id, "turn_id", turnID, "code", code)
	default:
		reply = ToSpoken(res.Text, c.g.phrases, maxSpokenRunes)
		if reply == "" {
			reply, code = c.g.phrases.AgentError, CodeAgentFailed
		}
	}
	c.mu.Lock()
	for i := range c.turns {
		if c.turns[i].TurnID == turnID {
			c.turns[i].Assistant, c.turns[i].Code = reply, code
		}
	}
	if c.currentTurn == turnID {
		c.turnCancel, c.turnRunning = nil, false
	}
	c.lastActivity = c.g.opts.Now()
	c.mu.Unlock()

	if err := c.speak(turnID, reply); err != nil && !errors.Is(err, errStale) && c.ctx.Err() == nil {
		c.mu.Lock()
		c.ttsFailures++
		failures := c.ttsFailures
		c.mu.Unlock()
		slog.Warn("Failed to speak voice reply", "call_id", c.id, "turn_id", turnID, "code", CodeOf(err))
		if failures >= 2 {
			c.finish(StateFailed, CodeProviderUnavailable)
		}
	}
}

// confirmer asks the caller to approve one action of turnID and waits for
// the next utterance.
func (c *call) confirmer(turnID string) Confirmer {
	return func(ctx context.Context, req ConfirmRequest) (bool, error) {
		w := &confirmWait{turnID: turnID, answer: make(chan string, 1)}
		c.mu.Lock()
		if c.currentTurn != turnID || c.ending {
			c.mu.Unlock()
			return false, nil
		}
		c.confirm = w
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			if c.confirm == w {
				c.confirm = nil
			}
			c.mu.Unlock()
		}()
		prompt := fmt.Sprintf(c.g.phrases.ConfirmPrompt, ToSpoken(req.Summary, c.g.phrases, 360))
		if err := c.speak(turnID, prompt); err != nil && !errors.Is(err, errStale) {
			return false, err
		}
		timeout := time.NewTimer(time.Duration(c.g.cfg.SilenceTimeoutSeconds) * time.Second)
		defer timeout.Stop()
		select {
		case ans := <-w.answer:
			ok := ParseAnswer(ans) == AnswerYes
			slog.Info("Voice confirmation answered", "call_id", c.id, "turn_id", turnID, "tool", req.ToolName, "approved", ok)
			return ok, nil
		case <-timeout.C:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// speak synthesizes text for turnID and streams it to the caller. It
// stops as soon as the turn is superseded or the caller barges in.
func (c *call) speak(turnID, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	c.speakMu.Lock()
	defer c.speakMu.Unlock()
	c.mu.Lock()
	if c.currentTurn != turnID || (c.ending && turnID != closingTurn) {
		c.mu.Unlock()
		return errStale
	}
	sctx, cancel := context.WithCancel(c.ctx)
	if turnID == closingTurn {
		sctx, cancel = context.WithTimeout(context.WithoutCancel(c.ctx), goodbyeTimeout)
	}
	c.speakCancel, c.speakingTurn = cancel, turnID
	c.mu.Unlock()
	defer cancel()
	c.machine.TransitionFrom(StateSpeaking, StateActive, StateListening, StateToolRunning)

	stillSpeaking := func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.speakingTurn == turnID && c.currentTurn == turnID
	}
	for _, seg := range SplitSegments(text) {
		c.mu.Lock()
		c.metrics.TTSCharacters += utf8.RuneCountInString(seg)
		c.mu.Unlock()
		err := c.g.opts.Synthesizer.Synthesize(sctx, SynthesisRequest{Text: seg, Language: c.g.cfg.Language, Encoding: c.ttsEnc, TurnID: turnID},
			func(f AudioFrame) error {
				if !stillSpeaking() {
					return errStale
				}
				f.TurnID = turnID
				if f.Encoding != c.leg.Encoding() {
					conv, err := Convert(f, c.leg.Encoding())
					if err != nil {
						return err
					}
					f = conv
				}
				c.mu.Lock()
				c.metrics.AudioOutBytes += int64(len(f.Data))
				if c.metrics.FirstAudioMS == 0 && turnID != openingTurn && turnID != closingTurn {
					c.metrics.FirstAudioMS = max(c.since(), 1)
				}
				c.mu.Unlock()
				return c.leg.Send(sctx, f)
			})
		if err != nil {
			if errors.Is(err, errStale) || sctx.Err() != nil || !stillSpeaking() {
				return errStale
			}
			c.clearSpeaking(turnID)
			return err
		}
	}
	if err := c.leg.Mark(sctx, turnID); err != nil {
		c.clearSpeaking(turnID)
	}
	return nil
}

func (c *call) clearSpeaking(turnID string) {
	c.mu.Lock()
	if c.speakingTurn == turnID {
		c.speakingTurn, c.speakCancel = "", nil
		c.lastActivity = c.g.opts.Now()
	}
	c.mu.Unlock()
	c.machine.TransitionFrom(StateListening, StateSpeaking)
}

// watchMarks notices when queued audio has finished playing.
func (c *call) watchMarks() {
	defer c.wg.Done()
	marks := c.leg.Marks()
	for {
		select {
		case <-c.ctx.Done():
			return
		case name, ok := <-marks:
			if !ok {
				return
			}
			c.mu.Lock()
			match := c.speakingTurn == name
			running := c.turnRunning
			c.mu.Unlock()
			if match {
				c.clearSpeaking(name)
				if running {
					c.machine.TransitionFrom(StateToolRunning, StateListening)
				}
			}
		}
	}
}

// watchdog enforces the call limit and the silence timeout and notices the
// transport going away.
func (c *call) watchdog(parent context.Context) {
	defer c.wg.Done()
	limit := time.NewTimer(c.limit)
	defer limit.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	silence := time.Duration(c.g.cfg.SilenceTimeoutSeconds) * time.Second
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-parent.Done():
			c.finish(StateEnded, "")
			return
		case <-c.leg.Done():
			c.finish(StateEnded, "")
			return
		case <-limit.C:
			c.sayAndEnd(c.g.phrases.TimeUp, StateBudgetExceeded, CodeBudgetExceeded)
			return
		case <-tick.C:
			c.mu.Lock()
			idle := !c.turnRunning && c.speakingTurn == "" && c.confirm == nil && !c.ending &&
				c.g.opts.Now().Sub(c.lastActivity) > silence
			c.mu.Unlock()
			if idle {
				c.sayAndEnd(c.g.phrases.Silence, StateEnded, CodeTimeout)
				return
			}
		}
	}
}

// sayAndEnd speaks a closing phrase (bounded in time) and ends the call.
// Turns in flight are cancelled first so nothing else is spoken.
func (c *call) sayAndEnd(phrase string, state State, code Code) {
	c.mu.Lock()
	if c.ending {
		c.mu.Unlock()
		return
	}
	c.ending = true
	if c.turnCancel != nil {
		c.turnCancel()
		c.markCancelledLocked(c.currentTurn)
	}
	if c.confirm != nil {
		close(c.confirm.answer)
		c.confirm = nil
	}
	c.currentTurn = closingTurn
	stop := c.speakCancel
	c.speakCancel, c.speakingTurn = nil, ""
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
	if c.ctx.Err() == nil {
		_ = c.speak(closingTurn, phrase)
		// Wait for the playback mark so the caller hears the phrase
		// before the line drops.
		deadline := time.Now().Add(goodbyeTimeout)
		for time.Now().Before(deadline) && c.ctx.Err() == nil {
			c.mu.Lock()
			done := c.speakingTurn != closingTurn
			c.mu.Unlock()
			if done {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	c.finish(state, code)
}
