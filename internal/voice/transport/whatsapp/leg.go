package whatsapp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

const (
	frameBytes      = 160 // 20 ms of 8 kHz mu-law
	frameDuration   = 20 * time.Millisecond
	maxBacklog      = 8000 * 300 // five minutes of queued audio
	audioBuffer     = 256
	markBuffer      = 32
	gatherTimeout   = 10 * time.Second
	connectTimeout  = 20 * time.Second
	disconnectGrace = 10 * time.Second
	mulawSilence    = 0xFF
)

// legOptions tunes a WebRTC peer.
type legOptions struct {
	stun []string
	// loopback lets two peers in one process find each other; tests only.
	loopback bool
}

// item is queued playback: audio, or a mark to report once the audio
// before it has been sent.
type item struct {
	data []byte
	mark string
}

// leg adapts one WebRTC peer to voice.CallLeg. Audio is G.711 mu-law at
// 8 kHz both ways; a pacer sends it in real-time 20 ms frames.
type leg struct {
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticSample

	audio  chan voice.AudioFrame
	marks  chan string
	done   chan struct{}
	conned chan struct{}

	audioMu     sync.Mutex
	audioClosed bool

	mu      sync.Mutex
	items   []item
	backlog int

	endOnce     sync.Once
	connOnce    sync.Once
	hangOnce    sync.Once
	remoteEnded atomic.Bool
	graceMu     sync.Mutex
	grace       *time.Timer

	// onHangup tells WhatsApp the call is over; set by the transport.
	onHangup func(context.Context)
}

func newLeg(opts legOptions) (*leg, error) {
	me := &webrtc.MediaEngine{}
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000, Channels: 1}
	if err := me.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: codec, PayloadType: 0}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(me, ir); err != nil {
		return nil, err
	}
	se := webrtc.SettingEngine{}
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	// WhatsApp cannot resolve .local names, so keep plain host candidates.
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	if opts.loopback {
		se.SetIncludeLoopbackCandidate(true)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithInterceptorRegistry(ir), webrtc.WithSettingEngine(se))
	cfg := webrtc.Configuration{}
	if len(opts.stun) > 0 {
		cfg.ICEServers = []webrtc.ICEServer{{URLs: opts.stun}}
	}
	pc, err := api.NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}
	track, err := webrtc.NewTrackLocalStaticSample(codec, "audio", "atlas")
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	l := &leg{
		pc: pc, track: track,
		audio: make(chan voice.AudioFrame, audioBuffer), marks: make(chan string, markBuffer),
		done: make(chan struct{}), conned: make(chan struct{}),
	}
	go func() {
		// RTCP must be read for the sender's interceptors to run.
		for {
			if _, _, err := sender.ReadRTCP(); err != nil {
				return
			}
		}
	}()
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) { go l.readTrack(tr) })
	pc.OnConnectionStateChange(l.onState)
	return l, nil
}

func (l *leg) onState(s webrtc.PeerConnectionState) {
	switch s {
	case webrtc.PeerConnectionStateConnected:
		l.graceMu.Lock()
		if l.grace != nil {
			l.grace.Stop()
			l.grace = nil
		}
		l.graceMu.Unlock()
		l.connOnce.Do(func() { close(l.conned) })
	case webrtc.PeerConnectionStateDisconnected:
		l.graceMu.Lock()
		if l.grace == nil {
			l.grace = time.AfterFunc(disconnectGrace, l.end)
		}
		l.graceMu.Unlock()
	case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
		l.end()
	}
}

// offer creates the local offer and returns it with every candidate.
func (l *leg) offer(ctx context.Context) (string, error) {
	desc, err := l.pc.CreateOffer(nil)
	if err != nil {
		return "", err
	}
	return l.setLocal(ctx, desc)
}

// answer applies a remote offer and returns the local answer with every
// candidate.
func (l *leg) answer(ctx context.Context, offerSDP string) (string, error) {
	if err := l.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		return "", err
	}
	desc, err := l.pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	return l.setLocal(ctx, desc)
}

// setAnswer applies the remote answer to our offer.
func (l *leg) setAnswer(answerSDP string) error {
	return l.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answerSDP})
}

func (l *leg) setLocal(ctx context.Context, desc webrtc.SessionDescription) (string, error) {
	gathered := webrtc.GatheringCompletePromise(l.pc)
	if err := l.pc.SetLocalDescription(desc); err != nil {
		return "", err
	}
	timer := time.NewTimer(gatherTimeout)
	defer timer.Stop()
	select {
	case <-gathered:
	case <-timer.C:
		// A slow STUN server must not fail the call; the candidates
		// found so far are enough for a reachable peer.
		slog.Warn("WhatsApp media candidate gathering timed out; continuing with what was found")
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return l.pc.LocalDescription().SDP, nil
}

// waitConnected blocks until media is up, the call ends or d passes.
func (l *leg) waitConnected(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-l.conned:
		return nil
	case <-l.done:
		return voice.ErrCallEnded
	case <-timer.C:
		return errors.New("media did not connect in time")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// start begins sending audio. It must wait until WhatsApp accepted the
// call, or the caller's first words are clipped.
func (l *leg) start() { go l.pace() }

func (l *leg) readTrack(tr *webrtc.TrackRemote) {
	if !strings.EqualFold(tr.Codec().MimeType, webrtc.MimeTypePCMU) {
		slog.Warn("Ignoring WhatsApp audio in an unsupported codec", "codec", tr.Codec().MimeType)
		return
	}
	for {
		pkt, _, err := tr.ReadRTP()
		if err != nil {
			return
		}
		if len(pkt.Payload) == 0 {
			continue
		}
		f := voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: append([]byte(nil), pkt.Payload...)}
		if f.Validate() != nil {
			continue
		}
		l.audioMu.Lock()
		if !l.audioClosed {
			select {
			case l.audio <- f:
			default:
				// Recognition is behind; dropping audio beats growing
				// latency on a live call.
			}
		}
		l.audioMu.Unlock()
	}
}

// pace sends one frame every 20 ms: queued audio, or silence so the media
// path stays alive between replies.
func (l *leg) pace() {
	tick := time.NewTicker(frameDuration)
	defer tick.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-tick.C:
		}
		_ = l.track.WriteSample(media.Sample{Data: l.next(), Duration: frameDuration})
	}
}

func (l *leg) next() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	buf := make([]byte, frameBytes)
	n := 0
	for n < frameBytes && len(l.items) > 0 {
		it := &l.items[0]
		if it.mark != "" {
			l.fireMarkLocked(it.mark)
			l.items = l.items[1:]
			continue
		}
		k := copy(buf[n:], it.data)
		n += k
		l.backlog -= k
		it.data = it.data[k:]
		if len(it.data) == 0 {
			l.items = l.items[1:]
		}
	}
	for i := n; i < frameBytes; i++ {
		buf[i] = mulawSilence
	}
	return buf
}

func (l *leg) fireMarkLocked(name string) {
	select {
	case l.marks <- name:
	default:
	}
}

func (l *leg) end() {
	l.endOnce.Do(func() {
		close(l.done)
		l.audioMu.Lock()
		l.audioClosed = true
		close(l.audio)
		l.audioMu.Unlock()
	})
}

func (l *leg) Encoding() voice.Encoding       { return voice.EncodingMulaw8k }
func (l *leg) Audio() <-chan voice.AudioFrame { return l.audio }
func (l *leg) Marks() <-chan string           { return l.marks }
func (l *leg) Done() <-chan struct{}          { return l.done }

func (l *leg) ended() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

func (l *leg) Send(_ context.Context, f voice.AudioFrame) error {
	if f.Encoding != voice.EncodingMulaw8k {
		return voice.Errorf(voice.CodeInvalidInput, "whatsapp call", errors.New("audio must be mu-law 8 kHz"))
	}
	if l.ended() {
		return voice.ErrCallEnded
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.backlog+len(f.Data) > maxBacklog {
		return voice.Errorf(voice.CodeTransportFailed, "whatsapp call", errors.New("playback backlog is full"))
	}
	l.items = append(l.items, item{data: append([]byte(nil), f.Data...)})
	l.backlog += len(f.Data)
	return nil
}

// Clear drops queued audio. Pending marks are reported at once, as
// Twilio does, so a waiter is never stranded.
func (l *leg) Clear(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, it := range l.items {
		if it.mark != "" {
			l.fireMarkLocked(it.mark)
		}
	}
	l.items, l.backlog = nil, 0
	return nil
}

func (l *leg) Mark(_ context.Context, name string) error {
	if l.ended() {
		return voice.ErrCallEnded
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = append(l.items, item{mark: name})
	return nil
}

// Hangup ends the call. Unless the other side ended it, WhatsApp is told
// so, then the media connection is closed. It is idempotent.
func (l *leg) Hangup(ctx context.Context) error {
	l.hangOnce.Do(func() {
		if !l.remoteEnded.Load() && l.onHangup != nil {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			l.onHangup(cctx)
			cancel()
		}
		_ = l.pc.Close()
	})
	l.end()
	return nil
}

// remoteHangup is Hangup for a call WhatsApp already reported as over.
func (l *leg) remoteHangup() {
	l.remoteEnded.Store(true)
	_ = l.Hangup(context.Background())
}
