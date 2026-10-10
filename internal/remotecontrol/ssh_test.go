package remotecontrol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// testSSHServer is a real SSH server on loopback. Commands are answered by
// handler, which sees the exact command line and standard input.
type testSSHServer struct {
	addr     string
	host     string
	port     int
	password string
	signer   ssh.Signer
	mu       sync.Mutex
	commands []string
	handler  func(cmd string, stdin []byte) (string, int)
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	return s
}

func startSSHServer(t *testing.T, signer ssh.Signer, password string) *testSSHServer {
	t.Helper()
	srv := &testSSHServer{password: password, signer: signer, handler: func(cmd string, stdin []byte) (string, int) {
		return "ran:" + cmd + "|stdin:" + string(stdin), 0
	}}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if string(pw) == srv.password {
				return nil, nil
			}
			return nil, io.ErrUnexpectedEOF
		},
	}
	cfg.AddHostKey(signer)
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	srv.addr = ln.Addr().String()
	srv.host = "127.0.0.1"
	srv.port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn, cfg)
		}
	}()
	return srv
}

func (s *testSSHServer) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "")
			continue
		}
		ch, requests, err := nc.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range requests {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				n := binary.BigEndian.Uint32(req.Payload[:4])
				cmd := string(req.Payload[4 : 4+n])
				_ = req.Reply(true, nil)
				stdin, _ := io.ReadAll(ch)
				s.mu.Lock()
				s.commands = append(s.commands, cmd)
				handler := s.handler
				s.mu.Unlock()
				out, code := handler(cmd, stdin)
				_, _ = io.WriteString(ch, out)
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, uint32(code))
				_, _ = ch.SendRequest("exit-status", false, status)
				return
			}
		}()
	}
}

func testProfile(srv *testSSHServer) config.RemoteProfile {
	return config.RemoteProfile{ID: "rp-0123456789ab", Name: "test", SSHHost: srv.host, SSHPort: srv.port, SSHUser: "atlas", SSHAuth: AuthPassword, PublicHost: "atlas.example.com", ConnectTimeout: 5}
}

func TestSSHFirstConnectionNeedsHostKeyDecisionThenRunsQuotedCommands(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user_known_hosts")
	require.NoError(t, os.WriteFile(userFile, []byte("# user file\n"), 0o600))
	keys := &HostKeys{Path: filepath.Join(dir, "known_hosts"), UserPath: userFile}
	srv := startSSHServer(t, newSigner(t), "s3cret-pass")
	d := &SSHDialer{HostKeys: keys}
	p := testProfile(srv)

	_, err := d.Dial(t.Context(), p, []byte("s3cret-pass"))
	decision, ok := DecisionFrom(err)
	require.True(t, ok, "first connection must stop for a decision: %v", err)
	require.False(t, decision.Changed)
	require.Equal(t, ssh.FingerprintSHA256(srv.signer.PublicKey()), decision.Fingerprint)
	require.NotContains(t, err.Error(), "s3cret-pass")
	_, statErr := os.Stat(keys.Path)
	require.ErrorIs(t, statErr, os.ErrNotExist, "nothing is trusted without a decision")

	require.Error(t, keys.Trust(decision.Address, decision.key, "SHA256:other"), "a decision binds to the fingerprint shown")
	require.NoError(t, keys.Trust(decision.Address, decision.key, decision.Fingerprint))
	r, err := d.Dial(t.Context(), p, []byte("s3cret-pass"))
	require.NoError(t, err)
	defer r.Close()
	require.Equal(t, "127.0.0.1", r.RemoteIP())
	res, err := r.Run(t.Context(), `echo "$1"`, []string{"a'b; reboot"}, []byte("payload"))
	require.NoError(t, err)
	require.Equal(t, `ran:sh -c 'echo "$1"' sh 'a'\''b; reboot'|stdin:payload`, string(res.Stdout))

	data, err := os.ReadFile(userFile)
	require.NoError(t, err)
	require.Equal(t, "# user file\n", string(data), "the user's known_hosts is never written")
}

func TestSSHChangedHostKeyStopsAndShowsBothFingerprints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keys := &HostKeys{Path: filepath.Join(dir, "known_hosts")}
	original := newSigner(t)
	srv := startSSHServer(t, original, "pw")
	p := testProfile(srv)
	address := HostAddress(p.SSHHost, p.Port())
	require.NoError(t, keys.Trust(address, original.PublicKey(), ssh.FingerprintSHA256(original.PublicKey())))

	impostor := startSSHServer(t, newSigner(t), "pw")
	impostor.handler = func(string, []byte) (string, int) {
		t.Error("a command reached a server with a changed key")
		return "", 1
	}
	p.SSHPort = impostor.port
	require.NoError(t, keys.Trust(HostAddress(p.SSHHost, p.Port()), original.PublicKey(), ssh.FingerprintSHA256(original.PublicKey())))
	_, err := (&SSHDialer{HostKeys: keys}).Dial(t.Context(), p, []byte("pw"))
	decision, ok := DecisionFrom(err)
	require.True(t, ok, err)
	require.True(t, decision.Changed)
	require.Equal(t, ssh.FingerprintSHA256(impostor.signer.PublicKey()), decision.Fingerprint)
	require.Equal(t, []string{"ssh-ed25519 " + ssh.FingerprintSHA256(original.PublicKey())}, decision.Previous)

	// Replacing is an explicit decision; afterwards only the new key works.
	require.NoError(t, keys.Trust(decision.Address, decision.key, decision.Fingerprint))
	impostor.handler = func(string, []byte) (string, int) { return "ok", 0 }
	r, err := (&SSHDialer{HostKeys: keys}).Dial(t.Context(), p, []byte("pw"))
	require.NoError(t, err)
	_ = r.Close()
	data, _ := os.ReadFile(keys.Path)
	require.Equal(t, 2, strings.Count(string(data), "\n"), "the replaced line is removed, the other host stays")
}

func TestSSHChangedKeyInUserKnownHostsIsReportedNotTrusted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := startSSHServer(t, newSigner(t), "pw")
	old := newSigner(t)
	userFile := filepath.Join(dir, "user_known_hosts")
	line := "[127.0.0.1]:" + strconv.Itoa(srv.port) + " " + string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(old.PublicKey())))
	require.NoError(t, os.WriteFile(userFile, []byte(line+"\n"), 0o600))
	keys := &HostKeys{Path: filepath.Join(dir, "known_hosts"), UserPath: userFile}
	_, err := (&SSHDialer{HostKeys: keys}).Dial(t.Context(), testProfile(srv), []byte("pw"))
	decision, ok := DecisionFrom(err)
	require.True(t, ok)
	require.True(t, decision.Changed)
	require.True(t, decision.InUserFile)

	p := testProfile(srv)
	p.IgnoreUserKnownHosts = true
	_, err = (&SSHDialer{HostKeys: keys}).Dial(t.Context(), p, []byte("pw"))
	decision, ok = DecisionFrom(err)
	require.True(t, ok)
	require.False(t, decision.Changed, "ignoring the user file makes it a first visit")
}

func TestSSHAuthenticationFailureTimeoutAndCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keys := &HostKeys{Path: filepath.Join(dir, "known_hosts")}
	srv := startSSHServer(t, newSigner(t), "right")
	p := testProfile(srv)
	require.NoError(t, keys.Trust(HostAddress(p.SSHHost, p.Port()), srv.signer.PublicKey(), ssh.FingerprintSHA256(srv.signer.PublicKey())))
	_, err := (&SSHDialer{HostKeys: keys}).Dial(t.Context(), p, []byte("wrong-password-value"))
	var e *Error
	require.ErrorAs(t, err, &e)
	require.Equal(t, "auth", e.Code)
	require.NotContains(t, err.Error(), "wrong-password-value")

	// A TCP listener that never speaks SSH must time out, not hang.
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	p.SSHPort, p.ConnectTimeout = ln.Addr().(*net.TCPAddr).Port, 1
	started := time.Now()
	_, err = (&SSHDialer{HostKeys: keys}).Dial(t.Context(), p, []byte("right"))
	require.ErrorAs(t, err, &e)
	require.Equal(t, "timeout", e.Code)
	require.Less(t, time.Since(started), 5*time.Second)

	p.SSHPort = srv.port
	p.ConnectTimeout = 5
	r, err := (&SSHDialer{HostKeys: keys}).Dial(t.Context(), p, []byte("right"))
	require.NoError(t, err)
	defer r.Close()
	block := make(chan struct{})
	srv.mu.Lock()
	srv.handler = func(string, []byte) (string, int) { <-block; return "", 0 }
	srv.mu.Unlock()
	t.Cleanup(func() { close(block) })
	ctx, cancel := contextWithTimeout(t, 200*time.Millisecond)
	defer cancel()
	_, err = r.Run(ctx, "sleep 100", nil, nil)
	require.ErrorAs(t, err, &e)
	require.Equal(t, "timeout", e.Code)
}

func TestSSHMissingPasswordAndKeyFileAreReportedBeforeConnecting(t *testing.T) {
	t.Parallel()
	keys := &HostKeys{Path: filepath.Join(t.TempDir(), "known_hosts")}
	p := config.RemoteProfile{ID: "rp-0123456789ab", SSHHost: "192.0.2.1", SSHUser: "u", SSHAuth: AuthPassword, ConnectTimeout: 1}
	_, err := (&SSHDialer{HostKeys: keys}).Dial(t.Context(), p, nil)
	var e *Error
	require.ErrorAs(t, err, &e)
	require.Equal(t, "password", e.Code)
	p.SSHAuth, p.SSHKeyFile = AuthKey, filepath.Join(t.TempDir(), "missing")
	_, err = (&SSHDialer{HostKeys: keys}).Dial(t.Context(), p, nil)
	require.ErrorAs(t, err, &e)
	require.Equal(t, "key_file", e.Code)
	p.SSHAuth = AuthAgent
	_, err = (&SSHDialer{HostKeys: keys, Agent: func() (io.ReadWriteCloser, error) { return nil, io.EOF }}).Dial(t.Context(), p, nil)
	require.ErrorAs(t, err, &e)
	require.Equal(t, "agent", e.Code)
}
