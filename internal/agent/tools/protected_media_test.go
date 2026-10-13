	l := newLiveTool(t)
	t.Cleanup(func() { vault.Ephemeral.PurgeSession(l.id) })
	require.NoError(t, vault.Register(l.id, "Synthetic-Image-Privacy-88!"))
	l.ok(`{"action":"navigate","url":"` + l.base + `/design.html"}`)
	l.tool = WithSecretFilter(l.tool)
	response := l.ok(`{"action":"screenshot","annotate":false}`)
	require.Equal(t, "image", response.Type)
	require.NotEmpty(t, response.Data)
	require.True(t, protectedCredentialMedia(BrowserToolName, response))
	// Non-browser media without a native redaction proof stays blocked.
	fixture := fantasy.NewAgentTool("unprotected_fixture", "Fixture.", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewImageResponse([]byte("unprotected-png"), "image/png"), nil
	})
	response, err := WithSecretFilter(fixture).Run(l.ctx, fantasy.ToolCall{Input: "{}"})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Empty(t, response.Data)
}
