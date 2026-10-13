		_, _, apiErr := s.resolveLaunch(t.Context(), req)
		require.NotNil(t, apiErr, name)
	}
	// A path is literal and never rewritten.
	argv, plan, apiErr := s.resolveLaunch(t.Context(), LaunchRequest{Argv: []string{`C:\Tools\x.exe`}})
	require.Nil(t, apiErr)
	require.Nil(t, plan)
	require.Equal(t, []string{`C:\Tools\x.exe`}, argv)
}
