package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if os.Getenv("ISOLATION_HELPER") == "sleep" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func capability(caps []Capability, backend string) Capability {
	for _, c := range caps {
		if c.Backend == backend {
			return c
		}
	}
	return Capability{}
}

func TestEvaluateReportsRealConstraints(t *testing.T) {
	t.Parallel()
	base := Facts{GOOS: "windows", Edition: "Professional", Build: 19045, SandboxExeFound: true, HypervisorPresent: true, VirtualizationFW: true}

	sb := capability(Evaluate(base), BackendWindowsSandbox)
	require.True(t, sb.Available)
	require.True(t, sb.Isolated)

	off := base
	off.HypervisorPresent, off.VirtualizationFW = false, false
	sb = capability(Evaluate(off), BackendWindowsSandbox)
	require.False(t, sb.Available)
	require.Contains(t, sb.Reason, "BIOS/UEFI")

	home := base
	home.Edition = "Core"
	require.False(t, capability(Evaluate(home), BackendWindowsSandbox).Available)

	missing := base
	missing.SandboxExeFound = false
	sb = capability(Evaluate(missing), BackendWindowsSandbox)
	require.False(t, sb.Available)
	require.True(t, sb.AdminRequired)

	busy := base
	busy.RunningSandboxPIDs = []int{42}
	sb = capability(Evaluate(busy), BackendWindowsSandbox)
	require.False(t, sb.Available)
	require.Contains(t, sb.Reason, "only one instance")

	cur := capability(Evaluate(base), BackendCurrentDesktop)
	require.False(t, cur.Isolated, "the user's own desktop must never be reported as isolated")
	require.False(t, capability(Evaluate(base), BackendHyperVVM).Available)
	require.False(t, capability(Evaluate(Facts{GOOS: "linux"}), BackendWindowsSandbox).Available)
}

func TestWSBEscapesPathsAndKeepsTheBinaryReadOnly(t *testing.T) {
	t.Parallel()
	xml, err := WSB(`C:\Users\A&B\bin`, `C:\Users\A&B\exchange`, StartOptions{})
	require.NoError(t, err)
	require.Contains(t, xml, `C:\Users\A&amp;B\bin`)
	require.NotContains(t, xml, `A&B`)
	require.Contains(t, xml, "<ReadOnly>true</ReadOnly>")
	require.Contains(t, xml, "<ClipboardRedirection>Disable</ClipboardRedirection>")
	require.Contains(t, xml, GuestBinDir+`\atlas.exe node serve --isolated-exchange `+GuestExchangeDir)
	// The bin mapping is the read-only one.
	bin := strings.Index(xml, `A&amp;B\bin`)
	ro := strings.Index(xml[bin:], "<ReadOnly>")
	require.True(t, strings.HasPrefix(xml[bin+ro:], "<ReadOnly>true"))
}

// fakePlatform simulates a guest: Launch starts an in-process node server
// provisioned from the exchange folder and a helper process standing in for
// the sandbox client.
type fakePlatform struct {
	t       *testing.T
	facts   Facts
	helper  *exec.Cmd
	server  *http.Server
	focused bool
}

func (f *fakePlatform) Probe(context.Context) Facts { return f.facts }

func (f *fakePlatform) Launch(_ context.Context, wsb string) ([]procown.Identity, error) {
	exchange := filepath.Join(filepath.Dir(wsb), "exchange")
	var prov provision
	data, err := os.ReadFile(filepath.Join(exchange, "provision.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &prov); err != nil {
		return nil, err
	}
	srv, err := nodes.NewServer(nodes.ServerOptions{StateDir: f.t.TempDir(), Root: filepath.Join(exchange, "files"), Capabilities: []nodes.Capability{nodes.CapExec, nodes.CapFiles}, Isolated: true})
	if err != nil {
		return nil, err
	}
	if _, err := srv.Auth().Provision("host", prov.Token); err != nil {
		return nil, err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f.server = &http.Server{Handler: srv.Handler(), TLSConfig: srv.TLSConfig()}
	go func() { _ = f.server.ServeTLS(ln, "", "") }()
	port := ln.Addr().(*net.TCPAddr).Port
	agent := agentInfo{LeaseID: prov.LeaseID, NodeID: srv.Identity().NodeID, Fingerprint: srv.Identity().Fingerprint, Port: port, Addresses: []string{"127.0.0.1"}}
	if err := taskstate.WriteJSONAtomic(filepath.Join(exchange, "agent.json"), agent); err != nil {
		return nil, err
	}
	exe, _ := os.Executable()
	// The fake guest lives until Stop ends it, not until a context ends.
	f.helper = exec.CommandContext(context.Background(), exe, "-test.run=^$")
	f.helper.Env = append(os.Environ(), "ISOLATION_HELPER=sleep")
	if err := f.helper.Start(); err != nil {
		return nil, err
	}
	created, err := procown.CreationTime(f.helper.Process.Pid)
	if err != nil {
		return nil, err
	}
	return []procown.Identity{{PID: f.helper.Process.Pid, CreatedAt: created}}, nil
}

func (f *fakePlatform) Focus([]procown.Identity) error { f.focused = true; return nil }

func (f *fakePlatform) HostSnapshot() (HostSnapshot, error) { return HostSnapshot{}, nil }

func availableFacts() Facts {
	return Facts{GOOS: "windows", Edition: "Professional", Build: 19045, SandboxExeFound: true, HypervisorPresent: true, VirtualizationFW: true}
}

func TestStartRefusesWithoutFallbackWhenUnavailable(t *testing.T) {
	t.Parallel()
	f := &fakePlatform{t: t, facts: Facts{GOOS: "windows", Edition: "Professional", Build: 19045, SandboxExeFound: true}}
	m := NewManager(t.TempDir(), os.Args[0], f)
	_, err := m.Start(t.Context(), StartOptions{})
	var ue *UnavailableError
	require.ErrorAs(t, err, &ue)
	require.True(t, errors.Is(err, ErrUnavailable))
	require.Nil(t, f.helper, "nothing may be launched when the backend is unavailable")
}

func TestLeaseLifecycleOwnsOnlyItsProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("starts helper processes")
	}
	devices := t.TempDir()
	f := &fakePlatform{t: t, facts: availableFacts()}
	exe, err := os.Executable()
	require.NoError(t, err)
	m := NewManager(devices, exe, f)
	m.ReadyTimeout = 20 * time.Second
	allowLoopbackGuest = true
	t.Cleanup(func() { allowLoopbackGuest = false; _ = f.server.Close() })

	// A process the user owns must survive Stop.
	bystander := exec.CommandContext(t.Context(), exe, "-test.run=^$")
	bystander.Env = append(os.Environ(), "ISOLATION_HELPER=sleep")
	require.NoError(t, bystander.Start())
	t.Cleanup(func() { _ = bystander.Process.Kill(); _, _ = bystander.Process.Wait() })

	st, err := m.Start(t.Context(), StartOptions{})
	require.NoError(t, err)
	require.Equal(t, taskstate.Running, st.Record.State)
	require.True(t, st.Alive)

	// The guest is reachable as an isolated node over the pinned channel.
	client, entry, err := nodes.OpenRegistry(devices).ClientFor(SandboxNodeName)
	require.NoError(t, err)
	require.True(t, entry.Isolated)
	info, err := client.Info(t.Context())
	require.NoError(t, err)
	require.True(t, info.Isolated)

	_, err = m.Start(t.Context(), StartOptions{})
	require.Error(t, err, "a second sandbox must not be started over a running one")

	st, err = m.Pause(st.Lease.ID, "user takes over")
	require.NoError(t, err)
	require.Equal(t, taskstate.Paused, st.Record.State)
	require.True(t, f.focused)
	st, err = m.Resume(st.Lease.ID)
	require.NoError(t, err)
	require.Equal(t, taskstate.Running, st.Record.State)

	st, err = m.Stop(st.Lease.ID)
	require.NoError(t, err)
	require.Equal(t, taskstate.Completed, st.Record.State)
	_, _ = f.helper.Process.Wait()
	require.NoDirExists(t, st.Lease.Dir)
	_, _, err = nodes.OpenRegistry(devices).Get(SandboxNodeName)
	require.Error(t, err)

	created, err := procown.CreationTime(bystander.Process.Pid)
	require.NoError(t, err, "a process Atlas did not start must keep running")
	require.NotZero(t, created)

	// A late resume after stop cannot revive the lease.
	_, err = m.Resume(st.Lease.ID)
	require.Error(t, err)
}

func TestSandboxClosedOutsideAtlasIsReportedFailed(t *testing.T) {
	if testing.Short() {
		t.Skip("starts helper processes")
	}
	devices := t.TempDir()
	f := &fakePlatform{t: t, facts: availableFacts()}
	exe, _ := os.Executable()
	m := NewManager(devices, exe, f)
	m.ReadyTimeout = 20 * time.Second
	allowLoopbackGuest = true
	t.Cleanup(func() { allowLoopbackGuest = false; _ = f.server.Close() })
	st, err := m.Start(t.Context(), StartOptions{})
	require.NoError(t, err)
	require.NoError(t, f.helper.Process.Kill())
	_, _ = f.helper.Process.Wait()
	require.Eventually(t, func() bool {
		s, err := m.Status(st.Lease.ID)
		return err == nil && s.Record.State == taskstate.Failed
	}, 5*time.Second, 100*time.Millisecond)
}
