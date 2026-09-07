package agent

// maxRetries is the retry budget the agent hands the provider library for
// one request.
//
// Unset (nil) leaves the library's own default in place rather than meaning
// "no retries": a config that never mentions retries should not silently
// turn them off. A configured 0 does disable them. The value is copied so
// the library cannot be handed a pointer into the agent's own state.
func (a *sessionAgent) maxRetries() *int {
	retries := a.maxProviderRetries.Get().v
	if retries == nil {
		return nil
	}
	n := max(*retries, 0)
	return &n
}
