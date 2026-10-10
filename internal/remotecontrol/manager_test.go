package remotecontrol

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

type harness struct {
	m        *Manager
	server   *fakeServer
	dialer   *fakeDialer
	approver *fakeApprover
	checker  *fakeChecker
	arts     fakeArtifacts
	p        config.RemoteProfile
	dir      string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		server: newFakeServer(), approver: &fakeApprover{answer: true}, checker: &fakeChecker{https: CheckOK, link: CheckOK},
		arts: fakeArtifacts{data: map[string][]byte{"amd64": []byte("relay-binary-v1"), "arm64": []byte("relay-binary-arm")}},
		dir:  t.TempDir(),
	}
	h.dialer = &fakeDialer{server: h.server}
	h.p = config.RemoteProfile{ID: NewProfileID(), Name: "vps", SSHHost: "203.0.113.10", SSHPort: 22, SSHUser: "ubuntu", SSHAuth: AuthAgent, PublicHost: "atlas.example.com"}
	h.m = NewManager(Options{
		Dir: h.dir, Dialer: h.dialer, Approver: h.approver, Verifier: h.checker, Artifacts: h.arts,
		Resolver:       staticResolver{"atlas.example.com": {"203.0.113.10"}},
		Logger:         slog.New(slog.DiscardHandler),
		HealthInterval: time.Millisecond, HealthAttempts: 4, HTTPSInterval: time.Millisecond, HTTPSAttempts: 2,
	})
	return h
}

func (h *harness) run(t *testing.T, start func(config.RemoteProfile) error) Snapshot {
	t.Helper()
	require.NoError(t, start(h.p))
	h.m.Wait()
	return h.m.Snapshot(h.p.ID)
}

func stepStatus(j *Journal) map[string]string {
	out := map[string]string{}
	for _, s := range j.Steps {
		out[s.ID] = s.Status
	}
	return out
}

func TestInstallShowsPlanOnceAppliesItAndVerifiesSeparately(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	snap := h.run(t, h.m.Install)
	require.Nil(t, snap.Err, "%v", snap.Err)
	require.Len(t, h.approver.requests, 1)
	req := h.approver.requests[0]
	require.Equal(t, OpInstall, req.Action)
	require.Equal(t, "ubuntu@203.0.113.10", req.Target)
	require.Equal(t, snap.Plan.Hash, req.Hash)
	for _, want := range []string{"atlas.example.com", "TCP 80 and 443", "/opt/atlas-mobile-relay", "DNS: not changed", "Rollback:"} {
		require.Contains(t, req.Summary, want)
	}
	require.Equal(t, StatusSucceeded, snap.Journal.Status)
	statuses := stepStatus(snap.Journal)
	for _, step := range []string{StepLock, StepBinary, StepKey, StepConfig, StepStart, StepHealth, StepState, StepUnlock, StepHTTPS, StepLink} {
		require.Equal(t, StatusDone, statuses[step], step)
	}
	require.Empty(t, h.server.lock, "the lock is released")
	require.Equal(t, "relay-binary-v1", string(h.server.files[RemoteDir+"/bin/atlas-mobile-relay"]))
	require.Equal(t, "atlas.example.com", h.server.state().PublicHost)
	require.Equal(t, sha256Hex([]byte("relay-binary-v1")), h.server.state().SHA256)
	keyPath, _ := h.m.KeyPath(h.p.ID)
	localKey, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.Equal(t, localKey, h.server.files[RemoteKeyFile], "the server receives this computer's connector key")
	require.Equal(t, CheckOK, snap.Checks[CheckHTTPS].State)
	require.Equal(t, CheckOK, snap.Checks[CheckLink].State)
	require.Equal(t, CheckOK, snap.Checks[CheckService].State)
	compose := string(h.server.files[RemoteDir+"/compose.yaml"])
	relay := compose[strings.Index(compose, "relay:"):strings.Index(compose, "caddy:")]
	require.NotContains(t, relay, "\n    ports:", "the relay listener is never published")
	require.Equal(t, "# Managed by Atlas /rc.\nATLAS_MOBILE_HOST=atlas.example.com\n", string(h.server.files[RemoteDir+"/.env"]))
}

func TestReinstallKeepsKeyAndSkipsUnchangedStepsWithoutRestarting(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.run(t, h.m.Install)
	firstKey := slices.Clone(h.server.files[RemoteKeyFile])
	snap := h.run(t, h.m.Install)
	require.Nil(t, snap.Err)
	require.Len(t, h.approver.requests, 2, "an update is a new scope and is approved again")
	require.True(t, snap.Plan.Update)
	require.False(t, snap.Plan.InstallKey)
	require.False(t, snap.Plan.ReplaceKey)
	statuses := stepStatus(snap.Journal)
	require.Equal(t, StatusSkipped, statuses[StepBinary], "an identical binary is not uploaded again")
	require.NotContains(t, statuses, StepKey)
	require.Equal(t, StatusDone, statuses[StepBackup])
	require.Equal(t, firstKey, h.server.files[RemoteKeyFile])
	require.Empty(t, h.server.restarts[len(h.server.restarts)-1], "nothing changed, so nothing is restarted")

	// A new binary restarts only the relay.
	h.arts.data["amd64"] = []byte("relay-binary-v2")
	snap = h.run(t, h.m.Install)
	require.Nil(t, snap.Err)
	require.Equal(t, []string{"relay"}, h.server.restarts[len(h.server.restarts)-1])
}

func TestDeniedApprovalChangesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.approver.answer = false
	snap := h.run(t, h.m.Install)
	require.Equal(t, "denied", snap.Err.Code)
	require.Empty(t, h.server.files)
	require.Equal(t, []string{"preflight"}, h.server.log, "only the read-only check ran")
}

func TestUnsupportedServersAreBlockedBeforeAnyChange(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*fakeServer){
		"os":        func(f *fakeServer) { f.osID, f.osName, f.osVersion = "centos", "CentOS Stream 9", "9" },
		"old":       func(f *fakeServer) { f.osVersion = "20.04" },
		"arch":      func(f *fakeServer) { f.machine = "armv7l" },
		"privilege": func(f *fakeServer) { f.privilege = "none" },
		"port":      func(f *fakeServer) { f.tcpListen = []string{"0.0.0.0:80", "[::]:443"} },
		"debian":    func(f *fakeServer) { f.osID, f.osVersion, f.docker = "debian", "12", false },
		"foreign":   func(f *fakeServer) { f.files[RemoteDir+"/foreign"] = []byte("x") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			mutate(h.server)
			snap := h.run(t, h.m.Install)
			require.Equal(t, "blocked", snap.Err.Code)
			require.NotEmpty(t, snap.Assessment.Blocking)
			require.Empty(t, h.approver.requests)
			require.Equal(t, []string{"preflight"}, h.server.log)
		})
	}
}

func TestMissingDNSBlocksAndMismatchWarns(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.m.opts.Resolver = staticResolver{}
	snap := h.run(t, h.m.Test)
	require.Equal(t, "blocked", snap.Err.Code)
	require.Contains(t, snap.Assessment.Blocking[0].String(), "no DNS A/AAAA record")

	h = newHarness(t)
	h.m.opts.Resolver = staticResolver{"atlas.example.com": {"198.51.100.7"}}
	snap = h.run(t, h.m.Test)
	require.Nil(t, snap.Err)
	require.True(t, slices.ContainsFunc(snap.Assessment.Warnings, func(m Message) bool { return strings.Contains(m.String(), "not to this server") }))
}

func TestMissingDockerOnUbuntuAndUFWAreInTheApprovedPlan(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.server.docker, h.server.ufw, h.server.ufwRules = false, "active", []string{"22/tcp"}
	snap := h.run(t, h.m.Install)
	require.Nil(t, snap.Err)
	require.Equal(t, []string{"docker.io", "docker-compose-v2"}, snap.Plan.Packages)
	require.Equal(t, []string{"80/tcp", "443/tcp", "443/udp"}, snap.Plan.FirewallRules)
	require.Equal(t, []string{"docker.io", "docker-compose-v2"}, h.server.packages)
	require.Equal(t, []string{"80/tcp", "443/tcp", "443/udp"}, h.server.firewall)
	require.Contains(t, h.approver.requests[0].Summary, "docker.io")
	require.Contains(t, h.approver.requests[0].Summary, "ufw rules are added")
}

func TestCanceledInstallReleasesLockAndResumesWithoutNewApproval(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.server.blockScript = "start"
	require.NoError(t, h.m.Install(h.p))
	require.Eventually(t, func() bool { return h.m.Snapshot(h.p.ID).OpStep == StepStart }, 5*time.Second, time.Millisecond)
	require.ErrorIs(t, h.m.Install(h.p), ErrBusy, "two installations never run at once")
	h.m.Cancel()
	h.m.Wait()
	snap := h.m.Snapshot(h.p.ID)
	require.Equal(t, "canceled", snap.Err.Code)
	require.Equal(t, StatusCanceled, snap.Journal.Status)
	require.Equal(t, StatusDone, stepStatus(snap.Journal)[StepBinary], "finished steps stay recorded")
	require.Empty(t, h.server.lock, "a canceled run releases the server")

	h.server.blockScript = ""
	snap = h.run(t, h.m.Install)
	require.Nil(t, snap.Err)
	require.Len(t, h.approver.requests, 1, "resuming the same plan does not ask again")
	require.Equal(t, StatusSucceeded, snap.Journal.Status)
}

func TestInterruptedJournalIsDetectedOnNextStart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	path, _ := h.m.journalPath(h.p.ID)
	j := &Journal{Kind: OpInstall, ProfileID: h.p.ID, Status: StatusRunning, Approved: true, PlanHash: "x", Started: time.Now()}
	j.Step(StepLock).Status = StatusDone
	j.Step(StepBinary).Status = StatusRunning
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, saveJournal(path, j))

	m := NewManager(Options{Dir: h.dir, Dialer: h.dialer})
	snap := m.Snapshot(h.p.ID)
	require.Equal(t, StatusInterrupted, snap.Journal.Status)
	require.Equal(t, StatusInterrupted, stepStatus(snap.Journal)[StepBinary])
	require.Equal(t, StatusDone, stepStatus(snap.Journal)[StepLock])
}

func TestFailedUpdateRestoresPreviousVersion(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.run(t, h.m.Install)
	h.arts.data["amd64"] = []byte("relay-binary-broken")
	h.server.badBinary = sha256Hex([]byte("relay-binary-broken"))
	snap := h.run(t, h.m.Install)
	require.Equal(t, "rolled_back", snap.Err.Code)
	require.Equal(t, StatusRolledBack, snap.Journal.Status)
	require.Equal(t, StatusDone, stepStatus(snap.Journal)[StepRollback])
	require.Equal(t, "relay-binary-v1", string(h.server.files[RemoteDir+"/bin/atlas-mobile-relay"]))
	require.Equal(t, "running", h.server.services["relay"])
	require.Empty(t, h.server.lock)
	require.Equal(t, sha256Hex([]byte("relay-binary-v1")), h.server.state().SHA256, "the recorded version is the restored one")

	// A first installation that fails has nothing to restore and says so.
	h = newHarness(t)
	h.server.badBinary = sha256Hex([]byte("relay-binary-v1"))
	snap = h.run(t, h.m.Install)
	require.Equal(t, "unhealthy", snap.Err.Code)
	require.Equal(t, StatusFailed, snap.Journal.Status)
	require.NotContains(t, stepStatus(snap.Journal), StepRollback)
}

func TestSuccessNeedsHTTPSAndRelayLinkNotJustExitCodes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.checker.https = CheckFailed
	snap := h.run(t, h.m.Install)
	require.Equal(t, "not_verified", snap.Err.Code)
	require.Equal(t, StatusIncomplete, snap.Journal.Status)
	require.Equal(t, CheckUnknown, snap.Checks[CheckLink].State, "the link is not claimed without HTTPS")
	require.Equal(t, 2, h.checker.calls, "HTTPS is retried while a certificate may be issued")

	h = newHarness(t)
	h.checker.link = CheckFailed
	snap = h.run(t, h.m.Install)
	require.Equal(t, "not_verified", snap.Err.Code)
	require.Equal(t, CheckOK, snap.Checks[CheckHTTPS].State)
	require.Equal(t, CheckFailed, snap.Checks[CheckLink].State)
}

func TestForeignLockBlocksAndStaleLockIsTakenOver(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.server.files[RemoteDir+"/.atlas-rc"] = []byte("x")
	h.server.lock = "someoneelse " + strconv.FormatInt(time.Now().Unix(), 10)
	snap := h.run(t, h.m.Install)
	require.Equal(t, "blocked", snap.Err.Code)
	require.Contains(t, snap.Assessment.Blocking[0].String(), "Another installation is in progress")

	h.server.lock = "someoneelse " + strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	rep := ParseReport([]byte(h.server.preflight()))
	rep.DNS.Matches = true
	a := Assess(rep, h.p, "", "mine", time.Now(), h.arts)
	require.True(t, a.ClearStaleLock)
	require.Empty(t, a.Blocking)
}

func TestHostKeyDecisionIsSurfacedWithoutRunningCommands(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.dialer.err = &HostKeyUnknownError{Address: "203.0.113.10", Algorithm: "ssh-ed25519", Fingerprint: "SHA256:abc"}
	snap := h.run(t, h.m.Test)
	require.NotNil(t, snap.HostKey)
	require.Equal(t, "SHA256:abc", snap.HostKey.Fingerprint)
	require.Equal(t, "host_key_unknown", snap.Err.Code)
	require.Equal(t, CheckFailed, snap.Checks[CheckSSH].State)
	require.Empty(t, h.server.log)

	h.dialer.err = &HostKeyChangedError{Address: "203.0.113.10", Algorithm: "ssh-ed25519", Fingerprint: "SHA256:new", Previous: []string{"ssh-ed25519 SHA256:old"}, Revoked: true}
	snap = h.run(t, h.m.Test)
	require.True(t, snap.HostKey.Changed)
	require.Error(t, h.m.TrustHostKey(h.p.ID, *snap.HostKey), "revoked keys are never trusted")
}

func TestSecretsNeverReachJournalsDiagnosticsOrErrors(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.m.Secrets.seal = func(b []byte) ([]byte, error) { return append([]byte("sealed:"), b...), nil }
	h.m.Secrets.unseal = func(b []byte) ([]byte, error) { return []byte(strings.TrimPrefix(string(b), "sealed:")), nil }
	h.p.SSHAuth, h.p.SecretRef = AuthPassword, NewSecretRef()
	persisted, err := h.m.Secrets.Put(h.p.SecretRef, []byte("hunter2-password"))
	require.NoError(t, err)
	require.True(t, persisted)
	// The fake server echoes a secret back, as a misbehaving command might.
	h.server.failScript = "start"
	snap := h.run(t, h.m.Install)
	require.NotNil(t, snap.Err)
	keyPath, _ := h.m.KeyPath(h.p.ID)
	key, _ := os.ReadFile(keyPath)
	token := strings.TrimSpace(string(key))
	journal, _ := os.ReadFile(filepath.Join(h.dir, "profiles", h.p.ID, "operation.json"))
	all := string(journal) + strings.Join(snap.Diagnostics, "\n") + snap.Err.Error() + h.approver.requests[0].Summary
	require.NotContains(t, all, "hunter2-password")
	require.NotContains(t, all, token)
	sealed, _ := os.ReadFile(filepath.Join(h.dir, "secrets", h.p.SecretRef+".sealed"))
	require.Equal(t, "sealed:hunter2-password", string(sealed), "only the sealed form is on disk")

	h.m.diag(h.p.ID, []string{"hunter2-password", token}, "echo hunter2-password "+token)
	require.NotContains(t, strings.Join(h.m.Snapshot(h.p.ID).Diagnostics, "\n"), "hunter2-password")
}

func TestSecretsFallBackToMemoryWhenSealingIsUnavailable(t *testing.T) {
	t.Parallel()
	s := &Secrets{Dir: t.TempDir(), seal: func([]byte) ([]byte, error) { return nil, os.ErrPermission }, unseal: func(b []byte) ([]byte, error) { return b, nil }}
	ref := NewSecretRef()
	persisted, err := s.Put(ref, []byte("pw"))
	require.NoError(t, err)
	require.False(t, persisted)
	got, err := s.Get(ref)
	require.NoError(t, err)
	require.Equal(t, "pw", string(got))
	entries, _ := os.ReadDir(s.Dir)
	require.Empty(t, entries, "nothing unsealed is written")
	require.NoError(t, s.Delete(ref))
	_, err = s.Get(ref)
	require.ErrorIs(t, err, ErrSecretMissing)
	_, err = s.Get("../escape")
	require.Error(t, err)
}

func TestRemoveServiceIsSeparateFromForgettingTheProfile(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.run(t, h.m.Install)
	snap := h.run(t, h.m.RemoveService)
	require.Nil(t, snap.Err)
	require.Equal(t, OpRemove, h.approver.requests[len(h.approver.requests)-1].Action)
	require.Contains(t, h.approver.requests[len(h.approver.requests)-1].Summary, "billing continues")
	require.Nil(t, h.server.files[RemoteKeyFile])
	require.False(t, h.server.installed())
	keyPath, _ := h.m.KeyPath(h.p.ID)
	_, err := os.Stat(keyPath)
	require.NoError(t, err, "removing the service keeps this computer's profile")

	require.NoError(t, h.m.ForgetProfile(h.p))
	_, err = os.Stat(keyPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	before := len(h.server.log)
	require.Equal(t, before, len(h.server.log), "forgetting a profile never contacts the server")
}

func TestRollbackRestoresNewestBackupAfterApproval(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.run(t, h.m.Install)
	h.arts.data["amd64"] = []byte("relay-binary-v2")
	h.run(t, h.m.Install)
	require.Equal(t, "relay-binary-v2", string(h.server.files[RemoteDir+"/bin/atlas-mobile-relay"]))
	snap := h.run(t, h.m.Rollback)
	require.Nil(t, snap.Err)
	require.Equal(t, OpRollback, h.approver.requests[len(h.approver.requests)-1].Action)
	require.Equal(t, "relay-binary-v1", string(h.server.files[RemoteDir+"/bin/atlas-mobile-relay"]))
	require.Equal(t, StatusSucceeded, snap.Journal.Status)
}
