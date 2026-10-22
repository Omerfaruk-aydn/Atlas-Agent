package agent

// activeContextWindow is the context window of the model that will actually
// answer the next request. After a fallback the chain's current model, not the
// configured primary, decides how much conversation fits and when to compact.
func activeContextWindow(model Model) int64 {
	return int64(model.CatwalkCfg.ContextWindow)
}

// fitsContextWindow reports whether a conversation of the given size can be
// sent to a model, leaving headroom for its output. An unknown window (0)
// never blocks.
func fitsContextWindow(model Model, promptTokens int64) bool {
	window := activeContextWindow(model)
	if window <= 0 {
		return true
	}
	headroom := int64(model.CatwalkCfg.DefaultMaxTokens)
	if model.ModelCfg.MaxTokens > 0 {
		headroom = model.ModelCfg.MaxTokens
	}
	return promptTokens+headroom <= window
}
