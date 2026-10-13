	require.Equal(t, "", expandCaptchaSecret("$NO_SUCH_VAR_AT_ALL"))
}

func boolPtr(b bool) *bool { return &b }
