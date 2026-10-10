package remotecontrol

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Result is the outcome of one remote command.
type Result struct {
	Stdout []byte
	Stderr []byte
	Exit   int
}

// Runner runs fixed scripts on a connected server.
type Runner interface {
	// Run executes script with sh. args are passed as positional
	// parameters, quoted; stdin carries file content.
	Run(ctx context.Context, script string, args []string, stdin []byte) (Result, error)
	// RemoteIP is the server address the connection reached.
	RemoteIP() string
	Close() error
}

// Dialer opens a Runner for a profile. secret is the password or key
// passphrase, or nil.
type Dialer interface {
	Dial(ctx context.Context, p config.RemoteProfile, secret []byte) (Runner, error)
}

// SSHDialer connects with golang.org/x/crypto/ssh. Host keys are always
// verified; there is no option to skip verification.
type SSHDialer struct {
	HostKeys *HostKeys
	// Agent opens the SSH agent; nil uses the platform default.
	Agent func() (io.ReadWriteCloser, error)
}

// Dial connects and authenticates within the profile's timeout.
func (d *SSHDialer) Dial(ctx context.Context, p config.RemoteProfile, secret []byte) (Runner, error) {
	timeout := time.Duration(p.Timeout()) * time.Second
	auth, closer, err := d.authMethods(ctx, p, secret)
	if err != nil {
		return nil, err
	}
	address := net.JoinHostPort(p.SSHHost, strconv.Itoa(p.Port()))
	var hostKeyErr error
	check := d.HostKeys.Callback(!p.IgnoreUserKnownHosts)
	cfg := &ssh.ClientConfig{
		User: p.SSHUser,
		Auth: auth,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			hostKeyErr = check(hostname, remote, key)
			return hostKeyErr
		},
		Timeout: timeout,
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		closeQuietly(closer)
		return nil, classifyDialError(ctx, err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(timeout))
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, cfg)
	stopped := stop()
	if err != nil {
		_ = conn.Close()
		closeQuietly(closer)
		if hostKeyErr != nil {
			return nil, hostKeyErr
		}
		if !stopped {
			return nil, classifyDialError(ctx, err)
		}
		return nil, classifyHandshakeError(err)
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(sshConn, chans, reqs)
	remote := ""
	if tcp, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		remote = tcp.IP.String()
	}
	return &sshRunner{client: client, closer: closer, remote: remote}, nil
}

func (d *SSHDialer) authMethods(ctx context.Context, p config.RemoteProfile, secret []byte) ([]ssh.AuthMethod, io.Closer, error) {
	switch p.SSHAuth {
	case AuthKey:
		if msg := CheckKeyFile(p.SSHKeyFile); !msg.IsZero() {
			return nil, nil, newError("key_file", msg, Msg("Check the key path in the profile."))
		}
		data, err := os.ReadFile(p.SSHKeyFile)
		if err != nil {
			return nil, nil, newError("key_file", Msg("The key file cannot be read."), Msg("Check the key path in the profile."))
		}
		defer clear(data)
		signer, err := ssh.ParsePrivateKey(data)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			if len(secret) == 0 {
				return nil, nil, newError("passphrase", Msg("The private key is protected by a passphrase."), Msg("Enter the passphrase in the profile."))
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, secret)
		}
		if err != nil {
			return nil, nil, newError("key_file", Msg("The private key could not be opened; the passphrase may be wrong or the format unsupported."), Msg("Check the key file and passphrase."))
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil, nil
	case AuthAgent:
		open := d.Agent
		if open == nil {
			open = func() (io.ReadWriteCloser, error) { return openAgent(ctx) }
		}
		conn, err := open()
		if err != nil {
			return nil, nil, newError("agent", Msg("No SSH agent is reachable."), Msg("Start ssh-agent and add your key, or choose another method."))
		}
		return []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(conn).Signers)}, conn, nil
	case AuthPassword:
		if len(secret) == 0 {
			return nil, nil, newError("password", Msg("No SSH password is stored for this profile."), Msg("Enter the password in the profile."))
		}
		password := string(secret)
		return []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					if !echos[i] {
						answers[i] = password
					}
				}
				return answers, nil
			}),
		}, nil, nil
	}
	return nil, nil, newError("auth_method", Msg("Choose key, agent or password."), Message{})
}

func closeQuietly(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

func timeoutError() error {
	return newError("timeout", Msg("The SSH connection timed out."), Msg("Check the address, port and firewall, or raise the timeout."))
}

func classifyDialError(ctx context.Context, err error) error {
	var netErr net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
		return timeoutError()
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return newError("canceled", Msg("Canceled."), Message{})
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return newError("dns", Msg("The server name could not be resolved."), Msg("Check the server address."))
	}
	if strings.Contains(strings.ToLower(err.Error()), "refused") {
		return newError("refused", Msg("The server refused the SSH connection."), Msg("Check that SSH runs on this port."))
	}
	return newError("network", Msg("The server could not be reached."), Msg("Check the address, port and your network."))
}

func classifyHandshakeError(err error) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "unable to authenticate"), strings.Contains(text, "no supported methods remain"):
		return newError("auth", Msg("The server rejected the SSH credentials."), Msg("Check the user name and authentication method."))
	case errors.Is(err, io.EOF), strings.Contains(text, "connection reset"):
		return newError("closed", Msg("The server closed the SSH connection during setup."), Msg("Check that this is an SSH server and that your address is not blocked."))
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return timeoutError()
	}
	return newError("handshake", Msg("The SSH handshake failed."), Msg("Open Diagnostics for details."))
}

type sshRunner struct {
	client *ssh.Client
	closer io.Closer
	remote string
	once   sync.Once
}

func (r *sshRunner) RemoteIP() string { return r.remote }

func (r *sshRunner) Close() error {
	var err error
	r.once.Do(func() {
		err = r.client.Close()
		closeQuietly(r.closer)
	})
	return err
}

// maxOutput bounds what a remote command can make Atlas buffer.
const maxOutput = 1 << 20

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxOutput - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (r *sshRunner) Run(ctx context.Context, script string, args []string, stdin []byte) (Result, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return Result{}, newError("session", Msg("The SSH connection was lost."), Msg("Try again."))
	}
	defer session.Close()
	var stdout, stderr limitedBuffer
	session.Stdout, session.Stderr = &stdout, &stderr
	session.Stdin = bytes.NewReader(stdin)
	if err := session.Start(ShellCommand(script, args...)); err != nil {
		return Result{}, newError("session", Msg("The SSH connection was lost."), Msg("Try again."))
	}
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		<-done
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Exit: -1}, newError("timeout", Msg("A remote step timed out."), Msg("Open Diagnostics for details."))
		}
		return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Exit: -1}, newError("canceled", Msg("Canceled."), Message{})
	case err := <-done:
		res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
		var exitErr *ssh.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr):
			res.Exit = exitErr.ExitStatus()
		default:
			return res, newError("session", Msg("The SSH connection was lost."), Msg("Try again."))
		}
		return res, nil
	}
}

// ShellCommand builds `sh -c '<script>' sh '<arg>'...` with every part in
// single quotes, so neither the script nor its arguments are reinterpreted
// by the login shell.
func ShellCommand(script string, args ...string) string {
	var b strings.Builder
	b.WriteString("sh -c ")
	b.WriteString(shellQuote(script))
	b.WriteString(" sh")
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
