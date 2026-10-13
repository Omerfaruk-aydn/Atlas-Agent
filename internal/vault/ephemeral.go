
// Typed errors let tools map refusals onto an actionable next step.
var (
	ErrCredentialUnknown  = errors.New("credential_unknown: the handle does not exist or already ended")
	ErrCredentialExpired  = errors.New("handle_expired: the temporary credential expired; request it again")
	ErrCredentialScope    = errors.New("scope_mismatch: the handle belongs to another task, origin or account")
	ErrCredentialNotReady = errors.New("credential_not_ready: the user has not supplied this credential yet")
	ErrCredentialUsed     = errors.New("credential_used: this temporary credential was already used once")
	ErrCredentialCanceled = errors.New("credential_canceled: the user or the task canceled this credential")
	ErrCredentialBudget   = errors.New("credential_budget: too many open credential requests")
	ErrNoInputChannel     = errors.New("secure_input_unavailable: no paired phone can receive a secure credential card")
)

// CredentialTarget is the page target observed when the request was made.
// A fill is dispatched only into this same tab, document and frame.
type CredentialTarget struct {
	TabID     string `json:"tab_id,omitempty"`
	Document  string `json:"document,omitempty"`
	FrameID   string `json:"frame_id,omitempty"`
	Selector  string `json:"selector,omitempty"`
	NodeToken string `json:"node_token,omitempty"`
}

// CredentialRequest is the non-secret view of a temporary credential. It
// is safe to show to the model, the phone and logs.
type CredentialRequest struct {
	ID            string           `json:"id"`
	SessionID     string           `json:"session_id"`
	InteractionID string           `json:"interaction_id,omitempty"`
	Service       string           `json:"service"`
	Origin        string           `json:"origin"`
	Account       string           `json:"account,omitempty"`
	State         CredentialState  `json:"state"`
	CreatedAt     time.Time        `json:"created_at"`
	ExpiresAt     time.Time        `json:"expires_at"`
	ProvidedBy    string           `json:"provided_by,omitempty"`
	Persist       bool             `json:"persist,omitempty"`
	Target        CredentialTarget `json:"-"`
}

// CredentialSpec describes a new request. Origin must be the page's real
// origin, observed by the tool, never a value copied from page text.
type CredentialSpec struct {
	SessionID     string
	InteractionID string
	Service       string
	Origin        string
	Account       string
	Target        CredentialTarget
}

// CredentialEvent notifies input channels (the phone gateway) about a
// changed request. It never carries the secret.
type CredentialEvent struct {
	Request CredentialRequest
}

type credentialEntry struct {
	req    CredentialRequest
	secret []byte
	timer  *time.Timer
	// changed is closed and replaced whenever the state moves, so a
	// waiting tool wakes without polling.
	changed chan struct{}
}

// Credentials is an in-memory store of temporary, target-bound secrets.
// Nothing is written to disk. Go cannot guarantee that every copy of a
// string or slice is zeroed under garbage collection; the store clears the
// buffers it owns and drops all references at the end of each lifetime.
type Credentials struct {
	mu       sync.Mutex
	entries  map[string]*credentialEntry
	channels map[int]func(CredentialEvent)
	nextSub  int
	now      func() time.Time
	chat     map[string]*chatCredential
	sources  map[string]map[string]time.Time
}

// Ephemeral is the process-wide temporary credential store.
var Ephemeral = NewCredentials()

// NewCredentials returns an empty store.
func NewCredentials() *Credentials {
	return &Credentials{entries: map[string]*credentialEntry{}, channels: map[int]func(CredentialEvent){}, now: time.Now, chat: map[string]*chatCredential{}, sources: map[string]map[string]time.Time{}}
}

// IsCredentialHandle reports whether id names a temporary credential
// rather than a saved vault item.
func IsCredentialHandle(id string) bool { return strings.HasPrefix(id, credentialHandleBits) }

// CredentialOrigin normalizes an exact origin. HTTPS is required except for
// loopback development hosts, which never leave this computer.
func CredentialOrigin(raw string) (string, error) {
	if o, err := Origin(raw); err == nil {
		return o, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("credential origin must be an exact HTTPS origin")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && !strings.HasSuffix(host, ".localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("credential origin must be an exact HTTPS origin")
	}
	return "http://" + strings.ToLower(u.Host), nil
}

// Subscribe registers an input channel. A request can be created only while
// at least one channel exists; when the last one leaves, pending requests
// are canceled so waiting tools stop instead of hanging.
func (c *Credentials) Subscribe(fn func(CredentialEvent)) func() {
	c.mu.Lock()
	c.nextSub++
	id := c.nextSub
	c.channels[id] = fn
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.channels, id)
		last := len(c.channels) == 0
		var events []CredentialEvent
		if last {
			for _, e := range c.entries {
				if (e.req.State == CredentialPending || e.req.State == CredentialProvided) && e.req.ProvidedBy != "user_prompt" {
					events = append(events, c.finishLocked(e, CredentialCanceled))
				}
			}
		}
		c.mu.Unlock()
		c.emit(events)
	}
}

// HasInputChannel reports whether a paired-device channel is listening.
func (c *Credentials) HasInputChannel() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.channels) > 0
}

// Request opens (or reuses) a credential card. An unconsumed request for
// the same task, origin and account is reused and rebound to the current
// target, so the user is not asked twice for the same authorization.
func (c *Credentials) Request(spec CredentialSpec) (CredentialRequest, bool, error) {
	origin, err := CredentialOrigin(spec.Origin)
	if err != nil {
		return CredentialRequest{}, false, err
	}
	if spec.SessionID == "" || len(spec.Service) > 120 || len(spec.Account) > 320 {
		return CredentialRequest{}, false, errors.New("invalid credential request")
	}
	service := strings.TrimSpace(spec.Service)
	if service == "" {
		service = strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	}
	account := strings.TrimSpace(spec.Account)
	c.mu.Lock()
	events := c.sweepLocked()
	if e := c.bindChatLocked(spec, origin, account, service); e != nil {
		view := e.req
		c.mu.Unlock()
		c.emit(events)
		return view, true, nil
	}
	pending := 0
	for _, e := range c.entries {
		if e.req.SessionID != spec.SessionID || (e.req.State != CredentialPending && e.req.State != CredentialProvided) {
			continue
		}
		if e.req.Origin == origin && strings.EqualFold(e.req.Account, account) {
			if e.req.Target != spec.Target {
				events = append(events, c.finishLocked(e, CredentialCanceled))
				continue
			}
			view := e.req
			c.mu.Unlock()
			c.emit(events)
			return view, true, nil
		}
		pending++
	}
	if len(c.channels) == 0 {
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, false, ErrNoInputChannel
	}
	if pending >= maxPendingPerSession || len(c.entries) >= maxCredentialsTotal {
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, false, ErrCredentialBudget
	}
	now := c.now()
	e := &credentialEntry{changed: make(chan struct{}), req: CredentialRequest{
		ID: credentialHandleBits + randomHandle(), SessionID: spec.SessionID, InteractionID: spec.InteractionID,
		Service: service, Origin: origin, Account: account, State: CredentialPending,
		CreatedAt: now, ExpiresAt: now.Add(CredentialRequestTTL), Target: spec.Target,
	}}
	c.entries[e.req.ID] = e
	c.scheduleExpiryLocked(e, CredentialRequestTTL)
	events = append(events, CredentialEvent{Request: e.req})
	view := e.req
	c.mu.Unlock()
	c.emit(events)
	return view, false, nil
}

// Get returns the non-secret view of a request.
func (c *Credentials) Get(id string) (CredentialRequest, bool) {
	c.mu.Lock()
	events := c.sweepLocked()
	e, ok := c.entries[id]
	var view CredentialRequest
	if ok {
		view = e.req
	}
	c.mu.Unlock()
	c.emit(events)
	return view, ok
}

// Pending lists open requests of a session ("" lists every session).
func (c *Credentials) Pending(sessionID string) []CredentialRequest {
	c.mu.Lock()
	events := c.sweepLocked()
	var out []CredentialRequest
	for _, e := range c.entries {
		if e.req.State == CredentialPending && (sessionID == "" || e.req.SessionID == sessionID) {
			out = append(out, e.req)
		}
	}
	c.mu.Unlock()
	c.emit(events)
	return out
}

// Provide stores the user's secret for one fill. The caller must have
// authenticated the device; deviceID is recorded so revoking that device
// also withdraws the secret. The store copies secret; callers clear theirs.
func (c *Credentials) Provide(id, sessionID, deviceID string, secret []byte, persist bool) (CredentialRequest, error) {
	if len(secret) == 0 || len(secret) > maxCredentialSecret {
		return CredentialRequest{}, errors.New("credential must be 1-4096 bytes")
	}
	c.mu.Lock()
	events := c.sweepLocked()
	e, ok := c.entries[id]
	switch {
	case !ok || e.req.SessionID != sessionID:
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, ErrCredentialUnknown
	case e.req.State == CredentialExpired:
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, ErrCredentialExpired
	case e.req.State == CredentialCanceled:
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, ErrCredentialCanceled
	case e.req.State != CredentialPending:
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, ErrCredentialUsed
	}
	if err := registerTemporary(sessionID, string(secret)); err != nil {
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, err
	}
	e.secret = append([]byte(nil), secret...)
	e.req.ProvidedBy = deviceID
	e.req.Persist = persist
	e.req.ExpiresAt = c.now().Add(CredentialSecretTTL)
	c.scheduleExpiryLocked(e, CredentialSecretTTL)
	events = append(events, c.transitionLocked(e, CredentialProvided))
	view := e.req
	c.mu.Unlock()
	// Later observations of this task never echo the value back.
	c.emit(events)
	return view, nil
}

// Cancel withdraws a request that has not been used.
func (c *Credentials) Cancel(id, sessionID string) (CredentialRequest, error) {
	c.mu.Lock()
	events := c.sweepLocked()
	e, ok := c.entries[id]
	if !ok || e.req.SessionID != sessionID {
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, ErrCredentialUnknown
	}
	if e.req.State != CredentialPending && e.req.State != CredentialProvided {
		view := e.req
		c.mu.Unlock()
		c.emit(events)
		return view, nil
	}
	events = append(events, c.finishLocked(e, CredentialCanceled))
	view := e.req
	c.mu.Unlock()
	c.emit(events)
	return view, nil
}

// Wait blocks until the request leaves the pending state, expires or ctx
// ends. It returns the final non-secret view.
func (c *Credentials) Wait(ctx context.Context, id string) (CredentialRequest, error) {
	for {
		c.mu.Lock()
		events := c.sweepLocked()
		e, ok := c.entries[id]
		if !ok {
			c.mu.Unlock()
			c.emit(events)
			return CredentialRequest{}, ErrCredentialUnknown
		}
		view, changed, deadline := e.req, e.changed, e.req.ExpiresAt
		c.mu.Unlock()
		c.emit(events)
		if view.State != CredentialPending {
			return view, nil
		}
		timer := time.NewTimer(max(time.Until(deadline), 10*time.Millisecond))
		select {
		case <-ctx.Done():
			timer.Stop()
			return view, ctx.Err()
		case <-changed:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// Use dispatches the secret once. The handle is marked consumed before
// fill runs, so a timeout or retry after dispatch can never send it twice.
// fill reports whether anything reached the page: a refusal before
// dispatch (stale target) returns the handle to the provided state.
func (c *Credentials) Use(id, sessionID, origin, account string, fill func(secret string) (dispatched bool, err error)) (CredentialRequest, error) {
	normalized, err := CredentialOrigin(origin)
	if err != nil {
		return CredentialRequest{}, ErrCredentialScope
	}
	c.mu.Lock()
	events := c.sweepLocked()
	e, ok := c.entries[id]
	var fail error
	switch {
	case !ok:
		fail = ErrCredentialUnknown
	case e.req.SessionID != sessionID || e.req.Origin != normalized || (account != "" && !strings.EqualFold(account, e.req.Account)):
		fail = ErrCredentialScope
	case e.req.State == CredentialExpired:
		fail = ErrCredentialExpired
	case e.req.State == CredentialCanceled:
		fail = ErrCredentialCanceled
	case e.req.State == CredentialConsumed:
		fail = ErrCredentialUsed
	case e.req.State == CredentialPending:
		fail = ErrCredentialNotReady
	}
	if fail != nil {
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, fail
	}
	secret := string(e.secret)
	e.req.State = CredentialConsumed
	c.mu.Unlock()
	c.emit(events)

	dispatched, fillErr := fill(secret)
	c.mu.Lock()
	if !dispatched && fillErr != nil && e.req.State == CredentialConsumed && c.now().Before(e.req.ExpiresAt) {
		// Nothing reached the page; the user's authorization still stands.
		e.req.State = CredentialProvided
		view := e.req
		c.mu.Unlock()
		return view, fillErr
	}
	event := c.finishLocked(e, CredentialConsumed)
	view := e.req
	c.mu.Unlock()
	c.emit([]CredentialEvent{event})
	return view, fillErr
}

// Target returns the bound target of an unconsumed request of a session.
func (c *Credentials) Target(id, sessionID string) (CredentialTarget, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok || e.req.SessionID != sessionID {
		return CredentialTarget{}, false
	}
	return e.req.Target, true
}

// PurgeSession ends every open credential of a session, for task
// cancellation or session deletion.
func (c *Credentials) PurgeSession(sessionID string) int {
	c.purgeChat(sessionID)
	return c.purge(func(r CredentialRequest) bool { return r.SessionID == sessionID })
}

// PurgeDevice ends every open credential supplied by a revoked device.
func (c *Credentials) PurgeDevice(deviceID string) int {
	if deviceID == "" {
		return 0
	}
	return c.purge(func(r CredentialRequest) bool { return r.ProvidedBy == deviceID })
}

// PurgeAll ends every open credential, for shutdown.
func (c *Credentials) PurgeAll() int {
	c.purgeChat("")
	return c.purge(func(CredentialRequest) bool { return true })
}

func (c *Credentials) purge(match func(CredentialRequest) bool) int {
	c.mu.Lock()
	var events []CredentialEvent
	for _, e := range c.entries {
		if (e.req.State == CredentialPending || e.req.State == CredentialProvided) && match(e.req) {
			events = append(events, c.finishLocked(e, CredentialCanceled))
		}
	}
	c.mu.Unlock()
	c.emit(events)
	return len(events)
}

// sweepLocked expires stale requests and forgets long-finished ones.
func (c *Credentials) sweepLocked() []CredentialEvent {
	now := c.now()
	var events []CredentialEvent
	for id, e := range c.entries {
		switch e.req.State {
		case CredentialPending, CredentialProvided:
			if !now.Before(e.req.ExpiresAt) {
				events = append(events, c.finishLocked(e, CredentialExpired))
			}
		default:
			if now.Sub(e.req.ExpiresAt) > 10*time.Minute {
				delete(c.entries, id)
			}
		}
	}
	return events
}

// finishLocked ends a lifetime: the secret buffer is cleared and dropped.
func (c *Credentials) finishLocked(e *credentialEntry, state CredentialState) CredentialEvent {
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	clear(e.secret)
	e.secret = nil
	if state != CredentialExpired {
		// Keep finished views briefly so late requests get a clear reason.
		e.req.ExpiresAt = c.now()
	}
	return c.transitionLocked(e, state)
}

// scheduleExpiryLocked releases credentials even when no new request arrives.
func (c *Credentials) scheduleExpiryLocked(e *credentialEntry, delay time.Duration) {
	if e.timer != nil {
		e.timer.Stop()
	}
	deadline := e.req.ExpiresAt
	e.timer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		if e.req.ExpiresAt != deadline || (e.req.State != CredentialPending && e.req.State != CredentialProvided) {
			c.mu.Unlock()
			return
		}
		event := c.finishLocked(e, CredentialExpired)
		c.mu.Unlock()
		c.emit([]CredentialEvent{event})
	})
}

func (c *Credentials) transitionLocked(e *credentialEntry, state CredentialState) CredentialEvent {
	e.req.State = state
	close(e.changed)
	e.changed = make(chan struct{})
	return CredentialEvent{Request: e.req}
}

func (c *Credentials) emit(events []CredentialEvent) {
	if len(events) == 0 {
		return
	}
	c.mu.Lock()
	subs := make([]func(CredentialEvent), 0, len(c.channels))
	for _, fn := range c.channels {
		subs = append(subs, fn)
	}
	c.mu.Unlock()
	for _, ev := range events {
		for _, fn := range subs {
			fn(ev)
		}
	}
}

func randomHandle() string {
	b := make([]byte, 15)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}
