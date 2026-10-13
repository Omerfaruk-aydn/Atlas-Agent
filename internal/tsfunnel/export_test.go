package tsfunnel

// FeedLine passes one CLI output line to a progress, for tests.
func FeedLine(p *Progress, s string) { p.line(s) }
