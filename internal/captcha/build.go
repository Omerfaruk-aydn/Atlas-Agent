	// AttemptTimeout bounds a single solver try.
	AttemptTimeout time.Duration
	// Audit receives one row per solver try when non-nil.
	Audit func(Attempt)
}

// Build assembles the solver chain from Options. The returned chain is
// never nil; when Enabled is false it has no solvers.
//
// Default chain: math (offline) + remote token farm when configured.
// Image-text challenges are not solved here at all -- the caller shows
// the challenge crop to the model, which reads it with its own vision.
func Build(opts Options) *Chain {
	if !opts.Enabled {
		return NewChain()
	}
	timeout := opts.AttemptTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	chain := NewChain().WithTimeout(timeout)
	if opts.Audit != nil {
		chain = chain.WithAudit(opts.Audit)
	}

	remoteAPIKey := firstNonEmpty(opts.RemoteAPIKey, os.Getenv("ATLAS_CAPTCHA_API_KEY"))
	remoteProvider := strings.ToLower(firstNonEmpty(opts.RemoteProvider, envOr("ATLAS_CAPTCHA_PROVIDER", "")))

	// Default order: offline math first, then remote token farm if a key
	// is present. No vision/OCR solver -- the model is the vision solver.
	defaultOrder := []string{"math", "remote"}
	order := opts.Solvers
	if len(order) == 0 {
		order = defaultOrder
	}

	for _, name := range order {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "math":
			chain.Add(MathSolver{})
		case "remote", "2captcha", "capsolver", "anticaptcha":
			provider := remoteProvider
			if name != "remote" {
				provider = name
			}
			if provider != "" && remoteAPIKey != "" {
				chain.Add(&RemoteSolver{
					Provider: RemoteProvider(provider),
					APIKey:   remoteAPIKey,
				})
			}
		}
	}
	return chain
}

// WithTimeout is a chain fluent alias used by Build.
func (c *Chain) WithTimeout(d time.Duration) *Chain { return c.WithAttemptTimeout(d) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
