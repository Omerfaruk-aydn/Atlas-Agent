package model

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/stretchr/testify/require"
)

type remoteConfigWorkspace struct {
	workspace.Workspace
	cfg     *config.Config
	set     map[string]any
	removed []string
}

func (w *remoteConfigWorkspace) Config() *config.Config { return w.cfg }

func (w *remoteConfigWorkspace) SetConfigField(_ config.Scope, key string, value any) error {
	w.set[key] = value
	switch key {
	case "tools.devices.remote_profiles":
		w.cfg.Tools.Devices.RemoteProfiles = value.([]config.RemoteProfile)
	case "tools.devices.remote_active":
		w.cfg.Tools.Devices.RemoteActive = value.(string)
	case "tools.devices.mobile_relay_url":
		w.cfg.Tools.Devices.MobileRelayURL = value.(string)
	}
	return nil
}

func (w *remoteConfigWorkspace) RemoveConfigField(_ config.Scope, key string) error {
	w.removed = append(w.removed, key)
	switch key {
	case "tools.devices.mobile_relay_url":
		w.cfg.Tools.Devices.MobileRelayURL = ""
	case "tools.devices.mobile_relay_token_file":
		w.cfg.Tools.Devices.MobileRelayTokenFile = ""
	}
	return nil
}

// The lifecycle tests never invoke workspace actions through the gateway.
type remoteLifecycleBackend struct{ mobile.Backend }

func newRemoteLifecycleUI() (*UI, *remoteConfigWorkspace) {
	sty := styles.AtlasPantera()
	p := config.RemoteProfile{ID: "test-vps", PublicHost: "atlas.example.com"}
	cfg := &config.Config{}
	cfg.Tools.Devices.RemoteProfiles = []config.RemoteProfile{p}
	cfg.Tools.Devices.RemoteActive = p.ID
	ws := &remoteConfigWorkspace{cfg: cfg, set: map[string]any{}}
	return &UI{com: &common.Common{Workspace: ws, Styles: &sty}, dialog: dialog.NewOverlay()}, ws
}

func newRemoteLifecycleGateway(t *testing.T) *mobile.Gateway {
	t.Helper()
	gw, err := mobile.Start(t.Context(), mobile.Options{
		StateDir: t.TempDir(), Listen: "127.0.0.1:0", Backend: remoteLifecycleBackend{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gw.Close() })
	return gw
}

func requireRemoteGatewayClosed(t *testing.T, gw *mobile.Gateway) {
	t.Helper()
	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	require.Eventually(t, func() bool {
		conn, err := dialer.DialContext(t.Context(), "tcp", strings.TrimPrefix(gw.URLs()[0], "https://"))
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	}, time.Second, 10*time.Millisecond, "obsolete gateway must release its listener")
}

func TestRemoteDisableInvalidatesQueuedEnable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ATLAS_DEVICES_DIR", dir)
	m, ws := newRemoteLifecycleUI()
	enable := m.enableRemote("test-vps")
	require.True(t, m.devices.remote.starting)
	disable := m.disableRemote()
	m.handleRemoteMsg(disable())
	require.Nil(t, enable(), "obsolete enable must not write config or start a gateway")
	require.Empty(t, ws.set)
	require.Empty(t, ws.cfg.Tools.Devices.MobileRelayURL)
	require.False(t, m.devices.remote.starting)
	require.Nil(t, m.devices.gateway)
}

func TestRemoteDisableDuringStartupClosesObsoleteGateway(t *testing.T) {
	t.Parallel()
	m, ws := newRemoteLifecycleUI()
	gw := newRemoteLifecycleGateway(t)
	r := &m.devices.remote
	generation := r.generation.Add(1)
	r.starting = true
	entered, release := make(chan struct{}), make(chan struct{})
	start := r.startCommand(generation, true, nil, func() (*mobile.Gateway, error) {
		close(entered)
		<-release
		return gw, nil
	})
	result := make(chan tea.Msg, 1)
	go func() { result <- start() }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("startup never entered")
	}
	disable := m.disableRemote()
	close(release)
	select {
	case msg := <-result:
		require.Nil(t, msg, "canceled startup must not publish a success")
	case <-time.After(5 * time.Second):
		t.Fatal("obsolete startup never completed")
	}
	m.handleRemoteMsg(disable())
	requireRemoteGatewayClosed(t, gw)
	require.Empty(t, ws.cfg.Tools.Devices.MobileRelayURL)
	require.Nil(t, r.attached.Load())
	require.False(t, r.starting)
}

func TestRemoteDelayedSuccessAfterDisableIsClosed(t *testing.T) {
	t.Parallel()
	m, _ := newRemoteLifecycleUI()
	gw := newRemoteLifecycleGateway(t)
	generation := m.devices.remote.generation.Add(1)
	m.devices.remote.starting = true
	msg := remoteGatewayMsg{generation: generation, gw: gw, auto: true}
	m.handleRemoteMsg(m.disableRemote()())
	cleanup := m.handleRemoteGateway(msg)
	require.NotNil(t, cleanup)
	require.Nil(t, m.devices.gateway)
	cleanup()
	requireRemoteGatewayClosed(t, gw)
	require.False(t, m.devices.remote.starting)
}

func TestRemoteOldResultsCannotChangeANewerConnection(t *testing.T) {
	t.Parallel()
	m, _ := newRemoteLifecycleUI()
	old := m.devices.remote.generation.Add(1)
	current := m.devices.remote.generation.Add(1)
	m.devices.remote.starting = true
	staleErr := errors.New("old connection failed")
	require.Nil(t, m.handleRemoteGateway(remoteGatewayMsg{generation: old, err: staleErr, auto: true}))
	require.True(t, m.devices.remote.starting)
	require.NoError(t, m.devices.remote.startErr)
	require.Zero(t, m.devices.remote.retry)
	require.Nil(t, m.handleRemoteMsg(remoteRetryMsg{generation: old}))
	require.Nil(t, m.handleRemoteMsg(remoteDisabledMsg{generation: old, err: staleErr}))
	gw := newRemoteLifecycleGateway(t)
	require.Nil(t, m.handleRemoteGateway(remoteGatewayMsg{generation: current, gw: gw, auto: true}))
	require.Same(t, gw, m.devices.gateway)
	require.Same(t, gw, m.devices.remote.attached.Load())
	require.False(t, m.devices.remote.starting)
}

func TestRemoteOldDisableDoesNotEraseNewSettings(t *testing.T) {
	t.Parallel()
	m, ws := newRemoteLifecycleUI()
	disable := m.disableRemote()
	r := &m.devices.remote
	generation := r.generation.Add(1)
	msg := r.startCommand(generation, false, nil, func() (*mobile.Gateway, error) {
		return nil, ws.SetConfigField(config.ScopeGlobal, "tools.devices.mobile_relay_url", "https://new.example.com")
	})().(remoteGatewayMsg)
	require.NoError(t, msg.err)
	require.Nil(t, disable())
	require.Equal(t, "https://new.example.com", ws.cfg.Tools.Devices.MobileRelayURL)
	require.Empty(t, ws.removed)
}

func TestMobileStopInvalidatesPendingRemoteStart(t *testing.T) {
	t.Parallel()
	m, _ := newRemoteLifecycleUI()
	generation := m.devices.remote.generation.Add(1)
	m.devices.remote.starting = true
	stop := m.stopMobile()
	require.False(t, m.devices.remote.starting)
	require.Greater(t, m.devices.remote.generation.Load(), generation)
	msg := stop().(mobileStoppedMsg)
	require.Equal(t, m.devices.remote.generation.Load(), msg.generation)
	require.Nil(t, m.handleRemoteGateway(remoteGatewayMsg{generation: generation, err: errors.New("stale"), auto: true}))
	require.Zero(t, m.devices.remote.retry)
}

func TestRemoteDisableRejectsDelayedMobileStart(t *testing.T) {
	t.Parallel()
	m, _ := newRemoteLifecycleUI()
	gw := newRemoteLifecycleGateway(t)
	generation := m.devices.remote.generation.Add(1)
	m.handleRemoteMsg(m.disableRemote()())
	cleanup := m.handleMobileReady(mobileReadyMsg{generation: generation, gw: gw})
	require.NotNil(t, cleanup)
	require.Nil(t, m.devices.gateway)
	require.False(t, m.dialog.ContainsDialog(dialog.MobileID))
	cleanup()
	requireRemoteGatewayClosed(t, gw)
}

func TestRemoteControlSlashCommandsOpenTheSameDialog(t *testing.T) {
	sty := styles.AtlasPantera()
	ws := &modelManagementWorkspace{cfg: &config.Config{Options: &config.Options{}}}
	m := &UI{com: &common.Common{Workspace: ws, Styles: &sty}, dialog: dialog.NewOverlay()}
	found := map[string]bool{}
	for _, item := range m.slashCommandItems() {
		if item.Name == "/rc" || item.Name == "/remote-control" {
			require.Equal(t, dialog.ActionOpenRemote{}, item.Action)
			found[item.Name] = true
		}
	}
	require.True(t, found["/rc"])
	require.True(t, found["/remote-control"])

	commands, err := m.newCommandsDialog()
	require.NoError(t, err)
	count := 0
	for _, c := range commands.AllItems() {
		if c.Action() == (dialog.ActionOpenRemote{}) {
			count++
		}
	}
	require.Equal(t, 1, count, "the palette lists Remote Control once")
}

func TestSavingAProfileKeepsSecretsOutOfConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ATLAS_DEVICES_DIR", dir)
	sty := styles.AtlasPantera()
	ws := &remoteConfigWorkspace{cfg: &config.Config{}, set: map[string]any{}}
	m := &UI{com: &common.Common{Workspace: ws, Styles: &sty}, dialog: dialog.NewOverlay()}
	key := filepath.Join(dir, "id_ed25519")
	require.NoError(t, os.WriteFile(key, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nnot-a-real-key\n"), 0o600))

	d := remotecontrol.Draft{ID: remotecontrol.NewProfileID(), Name: "vps", SSHHost: "203.0.113.10", SSHPort: "22", SSHUser: "root", SSHAuth: remotecontrol.AuthKey, SSHKeyFile: key, PublicHost: "Atlas.Example.com", Secret: "correct horse battery staple"}
	msg := m.saveRemoteProfile(d)().(remoteSavedMsg)
	require.NoError(t, msg.err)
	require.Empty(t, msg.errs)
	data, err := json.Marshal(ws.set)
	require.NoError(t, err)
	require.NotContains(t, string(data), "correct horse battery staple")
	require.NotContains(t, string(data), "not-a-real-key", "the private key is referenced, never copied")
	profiles := ws.cfg.Tools.Devices.RemoteProfiles
	require.Len(t, profiles, 1)
	require.Equal(t, "atlas.example.com", profiles[0].PublicHost)
	require.NotEmpty(t, profiles[0].SecretRef)
	require.Equal(t, profiles[0].ID, ws.cfg.Tools.Devices.RemoteActive)
	got, err := m.remoteManager().Secrets.Get(profiles[0].SecretRef)
	require.NoError(t, err)
	require.Equal(t, "correct horse battery staple", string(got))
	if msg.persisted {
		sealed, err := os.ReadFile(filepath.Join(dir, "remote", "secrets", profiles[0].SecretRef+".sealed"))
		require.NoError(t, err)
		require.NotContains(t, string(sealed), "correct horse battery staple")
	}

	// A missing key file is reported on its field and nothing is written.
	delete(ws.set, "tools.devices.remote_profiles")
	d.SSHKeyFile = filepath.Join(dir, "missing")
	msg = m.saveRemoteProfile(d)().(remoteSavedMsg)
	require.Contains(t, msg.errs, remotecontrol.FieldSSHKeyFile)
	require.NotContains(t, ws.set, "tools.devices.remote_profiles")

	// Switching to the agent drops the stored secret.
	d.SSHAuth, d.Secret, d.SSHKeyFile = remotecontrol.AuthAgent, "", ""
	msg = m.saveRemoteProfile(d)().(remoteSavedMsg)
	require.NoError(t, msg.err)
	require.Empty(t, ws.cfg.Tools.Devices.RemoteProfiles[0].SecretRef)
	_, err = m.remoteManager().Secrets.Get(profiles[0].SecretRef)
	require.Error(t, err)

	// Deleting the profile turns remote access off first when it is on.
	ws.cfg.Tools.Devices.MobileRelayURL = "https://atlas.example.com"
	cfgMsg := m.deleteRemoteProfile(d.ID)().(remoteConfigMsg)
	require.NoError(t, cfgMsg.err)
	require.Contains(t, ws.removed, "tools.devices.mobile_relay_url")
	require.Empty(t, ws.cfg.Tools.Devices.RemoteProfiles)
	require.Empty(t, ws.cfg.Tools.Devices.RemoteActive)
}

func TestRemoteDraftFromAssistantIsValidatedNotTrusted(t *testing.T) {
	t.Parallel()
	text := "Found it.\n```atlas-remote-draft\n{\"name\":\"old\",\"ssh_host\":\"198.51.100.1\",\"ssh_port\":22,\"ssh_user\":\"root\",\"public_host\":\"old.example.com\"}\n```\nCorrection:\n```atlas-remote-draft\n{\"name\":\"hetzner\",\"ssh_host\":\"203.0.113.10\",\"ssh_port\":2222,\"ssh_user\":\"ubuntu\",\"public_host\":\"atlas.example.com\"}\n```"
	d, ok := ParseRemoteDraft(text)
	require.True(t, ok)
	require.Equal(t, "hetzner", d.Name, "the last block wins")
	require.Equal(t, "2222", d.SSHPort)
	require.Empty(t, d.Secret)
	_, errs := d.Validate()
	require.Contains(t, errs, remotecontrol.FieldSSHKeyFile, "the user still chooses how to authenticate")

	for _, bad := range []string{
		"no block here",
		"```atlas-remote-draft\n{\"ssh_host\":\"x\",\"password\":\"hunter2\"}\n```",
		"```atlas-remote-draft\nnot json\n```",
		"```atlas-remote-draft\n{\"ssh_host\":\"x\"}",
	} {
		_, ok := ParseRemoteDraft(bad)
		require.False(t, ok, bad)
	}
	require.NotContains(t, browserHelpPrompt, "password\":", "the template never asks for secrets")
	require.True(t, strings.Contains(remoteDraftTemplate, "atlas-remote-draft"))
}
