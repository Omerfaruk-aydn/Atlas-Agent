	})
	require.NoError(t, err)
	require.NotZero(t, port)
	_, err = g.ServeFunnel(t.Context(), funnelHost, nil)
	require.Error(t, err, "one Funnel per gateway")
	require.ErrorContains(t, g.Close(), "residual", "a failed removal is reported")
	require.True(t, listening.Load(), "the entry is removed while its target still listens")
	_ = g.Close()
	require.EqualValues(t, 1, calls.Load())
	_, err = (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	require.Error(t, err, "the loopback target is closed")
}

func TestFunnelRequestsWithoutTheFunnelHeaderProveNothing(t *testing.T) {
	t.Parallel()
	f := newFunnelGateway(t, nil)
	jar, _ := cookiejar.New(nil)
	direct := &phone{t: t, g: f.g, base: "http://127.0.0.1:" + strconv.Itoa(f.g.FunnelLocalPort()), client: &http.Client{Jar: jar}}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, direct.base+"/", nil)
	req.Host = funnelHost
	resp, err := direct.client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.True(t, f.g.FunnelSeen().IsZero())
}

func TestServeFunnelAcceptsOnlyTSNetNames(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"atlas-pc.tail1234.ts.net", "Atlas-PC.tail1234.ts.net", "atlas-pc.tail1234.ts.net:8443", "atlas-pc.tail1234.ts.net:10000"} {
		_, err := checkFunnelHost(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{"", "*.ts.net", "ts.net", "x.ts.net", "evil.example", "atlas-pc.tail1234.ts.net:443", "atlas-pc.tail1234.ts.net:80", "atlas-pc.tail1234.ts.net.evil.com", "100.101.102.103", "atlas_pc.tail1234.ts.net", "https://atlas-pc.tail1234.ts.net"} {
		_, err := checkFunnelHost(bad)
		require.Error(t, err, bad)
	}
}
