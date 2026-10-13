	require.NoError(t, err)
	require.Equal(t, "true", value)
	resp = l.run(string(input))
	require.True(t, resp.IsError)
	_, ok := l.session().(browser.SecretFiller)
	require.True(t, ok)
}
