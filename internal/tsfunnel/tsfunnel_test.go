	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/tsfunnel"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/tsfunnel/fakecli"
)

func manager(t *testing.T, d *fakecli.Daemon) *tsfunnel.Manager {
	t.Helper()
	return &tsfunnel.Manager{
		CLI:             d,
		StateFile:       filepath.Join(t.TempDir(), "funnel.json"),
		Poll:            5 * time.Millisecond,
		ConfirmTimeout:  2 * time.Second,
		ApprovalTimeout: 3 * time.Second,
		CleanupTimeout:  300 * time.Millisecond,
	}
}

func ready(t *testing.T, d *fakecli.Daemon) tsfunnel.Readiness {
	t.Helper()
	r := tsfunnel.Inspect(t.Context(), d, nil)
	require.True(t, r.Ready(), "problem %q: %s", r.Problem, r.Detail)
	return r
}

func TestInspectStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(*fakecli.Daemon)
		svc   tsfunnel.ServiceState
		want  tsfunnel.Problem
	}{
		{"not installed", func(d *fakecli.Daemon) { d.Installed = false }, tsfunnel.ServiceUnknown, tsfunnel.ProblemNotInstalled},
		{"service missing", func(*fakecli.Daemon) {}, tsfunnel.ServiceMissing, tsfunnel.ProblemNotInstalled},
		{"service stopped", func(*fakecli.Daemon) {}, tsfunnel.ServiceStopped, tsfunnel.ProblemServiceStopped},
		{"daemon unreachable", func(d *fakecli.Daemon) { d.ServiceDown = true }, tsfunnel.ServiceUnknown, tsfunnel.ProblemServiceStopped},
		{"needs login", func(d *fakecli.Daemon) { d.Backend = "NeedsLogin" }, tsfunnel.ServiceRunning, tsfunnel.ProblemNeedsLogin},
		{"machine auth", func(d *fakecli.Daemon) { d.Backend = "NeedsMachineAuth" }, tsfunnel.ServiceRunning, tsfunnel.ProblemMachineAuth},
		{"stopped", func(d *fakecli.Daemon) { d.Backend = "Stopped" }, tsfunnel.ServiceRunning, tsfunnel.ProblemDisconnected},
		{"starting", func(d *fakecli.Daemon) { d.Backend = "Starting" }, tsfunnel.ServiceRunning, tsfunnel.ProblemStarting},
		{"old version", func(d *fakecli.Daemon) { d.Version = "1.50.1-t1" }, tsfunnel.ServiceRunning, tsfunnel.ProblemOldVersion},
		{"garbage version", func(d *fakecli.Daemon) { d.Version = "dev" }, tsfunnel.ServiceRunning, tsfunnel.ProblemOldVersion},
		{"no magicdns", func(d *fakecli.Daemon) { d.MagicDNS = false }, tsfunnel.ServiceRunning, tsfunnel.ProblemNeedsMagicDNS},
		{"custom domain", func(d *fakecli.Daemon) { d.Host = "pc.example.com." }, tsfunnel.ServiceRunning, tsfunnel.ProblemNeedsMagicDNS},
		{"bad json", func(d *fakecli.Daemon) { d.StatusRaw = []byte("{not json") }, tsfunnel.ServiceRunning, tsfunnel.ProblemMalformed},
		{"empty json", func(d *fakecli.Daemon) { d.StatusRaw = []byte("{}") }, tsfunnel.ServiceRunning, tsfunnel.ProblemMalformed},
		{"unknown backend", func(d *fakecli.Daemon) { d.Backend = "Weird" }, tsfunnel.ServiceRunning, tsfunnel.ProblemMalformed},
		{"no allowed port", func(d *fakecli.Daemon) {
			d.Caps = []string{"https", "funnel", "https://tailscale.com/cap/funnel-ports?ports=80,9000-9999"}
		}, tsfunnel.ServiceRunning, tsfunnel.ProblemNoFunnelPort},
		{"ready", func(*fakecli.Daemon) {}, tsfunnel.ServiceRunning, tsfunnel.ProblemNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := fakecli.New()
			tc.setup(d)
			r := tsfunnel.Inspect(t.Context(), d, func() tsfunnel.ServiceState { return tc.svc })
			require.Equal(t, tc.want, r.Problem, r.Detail)
		})
	}
}

func TestInspectReadsHostAndCapabilities(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.Caps = []string{"https", "funnel", "https://tailscale.com/cap/funnel-ports?ports=8443,10000"}
	r := ready(t, d)
	require.Equal(t, "atlas-pc.tail1234.ts.net", r.Host)
	require.Equal(t, []uint16{8443, 10000}, r.FunnelPorts)
	require.False(t, r.NeedsApproval())

	d.Caps = nil
	r = ready(t, d)
	require.True(t, r.NeedsApproval(), "missing https/funnel capabilities need the browser approval")
	require.Equal(t, tsfunnel.FunnelPorts, r.FunnelPorts)
}

func TestInspectTimeoutAndCancel(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.Delay = time.Minute
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := tsfunnel.Inspect(ctx, d, nil)
	require.Equal(t, tsfunnel.ProblemTimeout, r.Problem)
	require.Less(t, time.Since(start), 5*time.Second)

	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	r = tsfunnel.Inspect(ctx, d, nil)
	require.Equal(t, tsfunnel.ProblemFailed, r.Problem)
}

func TestPreparePicksFreePortAndLeavesOthersAlone(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.AddForeign("other-app", 443, "http://127.0.0.1:3000")
	m := manager(t, d)
	plan, err := m.Prepare(t.Context(), ready(t, d))
	require.NoError(t, err)
	require.Equal(t, uint16(8443), plan.Port)
	require.Equal(t, "https://atlas-pc.tail1234.ts.net:8443", plan.URL())
	require.Equal(t, "atlas-pc.tail1234.ts.net:8443", plan.HostHeader())

	d.SetBackground(map[string]any{"TCP": map[string]any{"8443": map[string]any{"HTTPS": true}, "10000": map[string]any{"TCPForward": "127.0.0.1:22"}}})
	_, err = m.Prepare(t.Context(), ready(t, d))
	var conflict *tsfunnel.PortConflictError
	require.ErrorAs(t, err, &conflict)
	require.Len(t, conflict.Occupants, 3)
	require.Equal(t, []string{"other-app"}, d.Sessions())
	for _, call := range d.Calls() {
		require.NotContains(t, call, "reset")
		require.NotEqual(t, "funnel", call[0]+strings.Join(call[1:2], ""), "prepare must not start a funnel")
	}
}

func TestPrepareRejectsUnready(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.Backend = "NeedsLogin"
	_, err := manager(t, d).Prepare(t.Context(), tsfunnel.Inspect(t.Context(), d, nil))
	require.Error(t, err)
}

func TestStartAndCloseOwnEntryOnly(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	background := map[string]any{"TCP": map[string]any{"10000": map[string]any{"TCPForward": "127.0.0.1:22"}}}
	d.SetBackground(background)
	d.AddForeign("other-app", 443, "http://127.0.0.1:3000")
	before := d.Background()
	m := manager(t, d)
	plan, err := m.Prepare(t.Context(), ready(t, d))
	require.NoError(t, err)
	progress := tsfunnel.NewProgress()
	link, err := m.Start(t.Context(), plan, 41234, progress)
	require.NoError(t, err)
	require.Equal(t, "https://atlas-pc.tail1234.ts.net:8443", link.URL())
	require.Equal(t, "http://127.0.0.1:41234", link.Target())
	phase, _, _ := progress.Snapshot()
	require.Equal(t, tsfunnel.PhaseReady, phase)
	require.FileExists(t, m.StateFile)
	data, err := os.ReadFile(m.StateFile)
	require.NoError(t, err)
	require.Contains(t, string(data), link.Session())
	require.Len(t, d.Sessions(), 2)

	require.NoError(t, link.Close())
	require.NoError(t, link.Close(), "close is idempotent")
	require.Equal(t, []string{"other-app"}, d.Sessions())
	require.JSONEq(t, string(before), string(d.Background()))
	require.NoFileExists(t, m.StateFile)
	phase, _, _ = progress.Snapshot()
	require.Equal(t, tsfunnel.PhaseStopped, phase)

	var funnels int
	for _, call := range d.Calls() {
		require.NotContains(t, call, "reset")
		require.NotContains(t, call, "--bg")
		require.NotContains(t, call, "off")
		if call[0] == "funnel" && call[1] != "status" {
			funnels++
			require.Equal(t, []string{"funnel", "--https=8443", "http://127.0.0.1:41234"}, call)
		}
	}
	require.Equal(t, 1, funnels)
}

func TestStartRejectsBadPlans(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	m := manager(t, d)
	for _, plan := range []tsfunnel.Plan{
		{Host: "atlas-pc.tail1234.ts.net", Port: 80},
		{Host: "evil.example.com", Port: 443},
		{Host: "", Port: 443},
	} {
		_, err := m.Start(t.Context(), plan, 41234, nil)
		require.Error(t, err)
	}
	_, err := m.Start(t.Context(), tsfunnel.Plan{Host: "atlas-pc.tail1234.ts.net", Port: 443}, 0, nil)
	require.Error(t, err)
	require.Empty(t, d.Procs())
}

func TestStartWaitsForBrowserApproval(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.Funnel = fakecli.FunnelApproval
	m := manager(t, d)
	progress := tsfunnel.NewProgress()
	type result struct {
		link *tsfunnel.Link
		err  error
	}
	done := make(chan result, 1)
	go func() {
		l, err := m.Start(t.Context(), tsfunnel.Plan{Host: "atlas-pc.tail1234.ts.net", Port: 443}, 5000, progress)
		done <- result{l, err}
	}()
	require.Eventually(t, func() bool {
		phase, u, _ := progress.Snapshot()
		return phase == tsfunnel.PhaseApproval && u == fakecli.ApprovalURL
	}, 2*time.Second, 5*time.Millisecond)
	d.Approve()
	res := <-done
	require.NoError(t, res.err)
	require.NoError(t, res.link.Close())
	require.Empty(t, d.Sessions())
}

func TestStartApprovalExitAndFailures(t *testing.T) {
	t.Parallel()
	plan := tsfunnel.Plan{Host: "atlas-pc.tail1234.ts.net", Port: 443}

	d := fakecli.New()
	d.Funnel = fakecli.FunnelApprovalExit
	_, err := manager(t, d).Start(t.Context(), plan, 5000, nil)
	var approval *tsfunnel.ApprovalRequiredError
	require.ErrorAs(t, err, &approval)
	require.Equal(t, fakecli.ApprovalURL, approval.URL)

	d = fakecli.New()
	d.Funnel = fakecli.FunnelFail
	_, err = manager(t, d).Start(t.Context(), plan, 5000, nil)
	require.ErrorContains(t, err, "funnel not available")

	d = fakecli.New()
	d.AddForeign("raced", 443, "http://127.0.0.1:9")
	_, err = manager(t, d).Start(t.Context(), plan, 5000, nil)
	var conflict *tsfunnel.PortConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, []string{"raced"}, d.Sessions())
}

func TestStartTimeoutAndCancelStopTheProcess(t *testing.T) {
	t.Parallel()
	plan := tsfunnel.Plan{Host: "atlas-pc.tail1234.ts.net", Port: 443}

	d := fakecli.New()
	d.Funnel = fakecli.FunnelHang
	m := manager(t, d)
	m.ConfirmTimeout = 50 * time.Millisecond
	_, err := m.Start(t.Context(), plan, 5000, nil)
	require.ErrorIs(t, err, tsfunnel.ErrTimeout)
	requireAllStopped(t, d)

	d = fakecli.New()
	d.Funnel = fakecli.FunnelApproval
	m = manager(t, d)
	ctx, cancel := context.WithCancel(t.Context())
	progress := tsfunnel.NewProgress()
	errc := make(chan error, 1)
	go func() {
		_, err := m.Start(ctx, plan, 5000, progress)
		errc <- err
	}()
	require.Eventually(t, func() bool { _, u, _ := progress.Snapshot(); return u != "" }, 2*time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-errc, context.Canceled)
	requireAllStopped(t, d)
	require.Empty(t, d.Sessions())
	require.NoFileExists(t, m.StateFile)
}

func requireAllStopped(t *testing.T, d *fakecli.Daemon) {
	t.Helper()
	for _, p := range d.Procs() {
		select {
		case <-p.Done():
		case <-time.After(2 * time.Second):
			t.Fatalf("process %v still runs", p.Args)
		}
	}
}

func TestLinkReportsLostProcess(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	m := manager(t, d)
	link, err := m.Start(t.Context(), tsfunnel.Plan{Host: "atlas-pc.tail1234.ts.net", Port: 443}, 5000, nil)
	require.NoError(t, err)
	d.Procs()[0].Crash()
	require.Eventually(t, func() bool {
		phase, _, _ := link.Progress().Snapshot()
		return phase == tsfunnel.PhaseLost
	}, 2*time.Second, 5*time.Millisecond)
	require.False(t, link.Alive())
	require.NoError(t, link.Close())
}

func TestCloseReportsResidualEntry(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.KeepSessions = true
	m := manager(t, d)
	link, err := m.Start(t.Context(), tsfunnel.Plan{Host: "atlas-pc.tail1234.ts.net", Port: 443}, 5000, nil)
	require.NoError(t, err)
	require.ErrorIs(t, link.Close(), tsfunnel.ErrResidual)
	require.FileExists(t, m.StateFile, "the record stays so the next start can retry cleanup")

	// A later start refuses to add a second entry while the old one is
	// listed, and never removes it by force.
	_, err = m.Prepare(t.Context(), ready(t, d))
	require.ErrorIs(t, err, tsfunnel.ErrResidual)
	require.Len(t, d.Sessions(), 1)
}

func TestRecoverAfterCrash(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	m := manager(t, d)
	link, err := m.Start(t.Context(), tsfunnel.Plan{Host: "atlas-pc.tail1234.ts.net", Port: 443}, 5000, nil)
	require.NoError(t, err)
	// Simulate Atlas dying: the CLI ends (job object) but Close never ran.
	d.Procs()[0].Crash()
	require.FileExists(t, m.StateFile)
	_ = link

	m2 := manager(t, d)
	m2.StateFile = m.StateFile
	plan, err := m2.Prepare(t.Context(), ready(t, d))
	require.NoError(t, err)
	require.Equal(t, uint16(443), plan.Port)
	require.NoFileExists(t, m.StateFile)
}

func TestRecoverIgnoresDamagedRecord(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	m := manager(t, d)
	require.NoError(t, os.WriteFile(m.StateFile, []byte("{broken"), 0o600))
	require.NoError(t, m.Recover(t.Context()))
	require.NoFileExists(t, m.StateFile)
}

func TestPrepareRejectsMalformedServeStatus(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.ServeRaw = []byte(`{"TCP":{"notaport":{}}}`)
	_, err := manager(t, d).Prepare(t.Context(), ready(t, d))
	require.ErrorIs(t, err, tsfunnel.ErrMalformed)
	d.ServeRaw = []byte(`[1,2`)
	_, err = manager(t, d).Prepare(t.Context(), ready(t, d))
	require.ErrorIs(t, err, tsfunnel.ErrMalformed)
}

func TestLoginOpensOnlyTailscaleURL(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.Backend = "NeedsLogin"
	var opened atomic.Value
	errc := make(chan error, 1)
	go func() {
		errc <- tsfunnel.Login(t.Context(), d, nil, func(u string) error { opened.Store(u); return nil })
	}()
	require.Eventually(t, func() bool { return opened.Load() != nil }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, fakecli.LoginURL, opened.Load())
	d.CompleteLogin()
	require.NoError(t, <-errc)
	require.Equal(t, []string{"up"}, d.Calls()[0], "login keeps the current profile: no 'login', no flags")

	d = fakecli.New()
	d.LoginFails = "Error: changing settings via 'tailscale up' requires mentioning all non-default flags"
	err := tsfunnel.Login(t.Context(), d, nil, nil)
	require.ErrorContains(t, err, "custom Tailscale settings")
}

func TestLoginCancel(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() { errc <- tsfunnel.Login(ctx, d, nil, nil) }()
	require.Eventually(t, func() bool { return len(d.Procs()) == 1 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-errc, context.Canceled)
	requireAllStopped(t, d)
}

func TestProgressIgnoresForeignURLs(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.Funnel = fakecli.FunnelApproval
	// A URL on any other host must never become the approval link.
	for _, u := range []string{"https://evil.example/login.tailscale.com", "http://login.tailscale.com/a/x", "https://login.tailscale.com.evil.net/a", "https://user@login.tailscale.com/a"} {
		p := tsfunnel.NewProgress()
		tsfunnel.FeedLine(p, "To authenticate, visit: "+u)
		_, got, _ := p.Snapshot()
		require.Empty(t, got, u)
	}
	p := tsfunnel.NewProgress()
	tsfunnel.FeedLine(p, "visit https://login.tailscale.com/a/abc.")
	_, got, _ := p.Snapshot()
	require.Equal(t, "https://login.tailscale.com/a/abc", got)
}

func TestSanitize(t *testing.T) {
	t.Parallel()
	require.Equal(t, "key [redacted] bad", tsfunnel.Sanitize("key tskey-auth-abc123\x1b[0m bad"))
	require.Equal(t, "a b", tsfunnel.Sanitize("a\nb"))
	require.LessOrEqual(t, len(tsfunnel.Sanitize(strings.Repeat("x", 1000))), 304)
}

func TestCLINotInstalled(t *testing.T) {
	t.Parallel()
	cli := tsfunnel.CLI{Binary: filepath.Join(t.TempDir(), "missing.exe")}
	_, err := cli.Output(t.Context(), "status")
	require.ErrorIs(t, err, tsfunnel.ErrNotInstalled)
	r := tsfunnel.Inspect(t.Context(), cli, nil)
	require.Equal(t, tsfunnel.ProblemNotInstalled, r.Problem)
}

func TestSignedByTailscale(t *testing.T) {
	t.Parallel()
	require.True(t, tsfunnel.SignedByTailscale(`CN="Tailscale Inc.", O="Tailscale Inc.", L=Toronto, S=Ontario, C=CA`))
	require.True(t, tsfunnel.SignedByTailscale(`CN=Tailscale Inc., O=Tailscale Inc., C=CA`))
	require.False(t, tsfunnel.SignedByTailscale(`CN=Tailscale Inc. Evil, O=Evil`))
	require.False(t, tsfunnel.SignedByTailscale(`CN=Evil, OU=Tailscale Inc.`))
	require.False(t, tsfunnel.SignedByTailscale(""))
}

// packageServer serves a fake package index with one MSI.
func packageServer(t *testing.T, msi []byte, sum string, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/stable/" && r.URL.RawQuery == "mode=json":
			_, _ = w.Write([]byte(`{"MSIsVersion":"1.104.1","MSIs":{"amd64":"` + name + `"}}`))
		case r.URL.Path == "/stable/"+name+".sha256":
			_, _ = w.Write([]byte(sum + "\n"))
		case r.URL.Path == "/stable/"+name:
			_, _ = w.Write(msi)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallerVerifiesBeforeRunning(t *testing.T) {
	t.Parallel()
	msi := []byte("fake msi payload")
	h := sha256.Sum256(msi)
	good := hex.EncodeToString(h[:])
	const name = "tailscale-setup-1.104.1-amd64.msi"

	run := func(t *testing.T, srv *httptest.Server, sig tsfunnel.Signature) (bool, error) {
		t.Helper()
		var ran bool
		in := tsfunnel.Installer{
			Client: srv.Client(), BaseURL: srv.URL + "/stable/", Arch: "amd64", TempRoot: t.TempDir(),
			Verify: func(_ context.Context, path string) (tsfunnel.Signature, error) {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, msi, data)
				return sig, nil
			},
			Run: func(context.Context, string) error { ran = true; return nil },
		}
		return ran, in.Install(t.Context())
	}
	valid := tsfunnel.Signature{Status: "Valid", Subject: "CN=Tailscale Inc., O=Tailscale Inc."}

	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, good, name), valid)
		require.NoError(t, err)
		require.True(t, ran)
	})
	t.Run("hash mismatch", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, strings.Repeat("0", 64), name), valid)
		require.ErrorContains(t, err, "does not match")
		require.False(t, ran)
	})
	t.Run("missing hash", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, "", name), valid)
		require.Error(t, err)
		require.False(t, ran)
	})
	t.Run("bad signature", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, good, name), tsfunnel.Signature{Status: "HashMismatch", Subject: valid.Subject})
		require.ErrorContains(t, err, "not Valid")
		require.False(t, ran)
	})
	t.Run("wrong signer", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, good, name), tsfunnel.Signature{Status: "Valid", Subject: "CN=Someone Else"})
		require.ErrorContains(t, err, "not Tailscale")
		require.False(t, ran)
	})
	t.Run("suspicious name", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, good, `..\evil.msi`), valid)
		require.ErrorIs(t, err, tsfunnel.ErrMalformed)
		require.False(t, ran)
	})
	t.Run("plain http index", func(t *testing.T) {
		t.Parallel()
		in := tsfunnel.Installer{BaseURL: "http://pkgs.example/stable/", Verify: func(context.Context, string) (tsfunnel.Signature, error) { return valid, nil }, Run: func(context.Context, string) error { return errors.New("must not run") }}
		require.ErrorContains(t, in.Install(t.Context()), "https")
	})
}
