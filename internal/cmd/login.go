package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clipboard"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/antigravity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/augment"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/claude"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/coderabbit"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/codex"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/copilot"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/factory"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/grok"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/jetbrains"
	museoauth "github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/muse"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/windsurf"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/zed"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Aliases: []string{"auth"},
	Use:     "login [platform]",
	Short:   "Login ATLAS-AGENT to a platform",
	Long: `Login ATLAS-AGENT to a specified platform.
	The platform should be provided as an argument.
	Available platforms are: copilot, chatgpt, antigravity, claude, grok,
	windsurf, jetbrains, muse.

	Note: antigravity only supports its Gemini-family models
	(gemini-3-pro-high/low); Claude and GPT-OSS models served through an
	Antigravity account are not supported.

	Note: claude, grok, windsurf, and jetbrains are coding-plan OAuth
	scaffolds. They are wired into the login flow and the model picker,
	but their model call layer is a stub that returns "not implemented"
	until the real request envelopes are captured against the official
	clients (see the package docs in internal/oauth/<plan>).`,
	Example: `
# Authenticate with GitHub Copilot
atlas login copilot

# Authenticate with a ChatGPT subscription
atlas login chatgpt

# Authenticate with a Google Antigravity (AI Pro/Ultra) account
atlas login antigravity

# Authenticate with a Claude Pro/Max/Team account
atlas login claude

# Authenticate with an xAI SuperGrok account
atlas login grok

# Authenticate with a Windsurf (Codeium) Pro/Teams account
atlas login windsurf

# Authenticate with a JetBrains AI Pro/Ultimate account
atlas login jetbrains

# Authenticate with a Meta Muse Code subscription
atlas login muse

# Force re-authentication even if already logged in
atlas login -f copilot
  `,
	ValidArgs: []cobra.Completion{
		"copilot",
		"github",
		"github-copilot",
		"chatgpt",
		"codex",
		"antigravity",
		"claude",
		"grok",
		"windsurf",
		"jetbrains",
		"augment",
		"factory",
		"coderabbit",
		"zed",
		"muse",
		"muse-code",
	},
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, cleanup, err := setupWorkspaceWithProgressBar(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		provider := "copilot"
		if len(args) > 0 {
			provider = args[0]
		}
		force, _ := cmd.Flags().GetBool("force")
		switch provider {
		case "copilot", "github", "github-copilot":
			return loginCopilot(ws, force)
		case "chatgpt", "codex":
			return loginChatGPT(ws, force)
		case "antigravity":
			return loginAntigravity(ws, force)
		case "claude", "claude-plan":
			return loginClaude(ws, force)
		case "grok", "grok-web", "supergrok":
			return loginGrokWeb(ws, force)
		case "windsurf", "codeium":
			return loginWindsurf(ws, force)
		case "jetbrains", "jb-ai":
			return loginJetBrains(ws, force)
		case "augment", "augmentcode":
			return loginAugment(ws, force)
		case "factory", "factory-ai":
			return loginFactory(ws, force)
		case "coderabbit":
			return loginCodeRabbit(ws, force)
		case "zed":
			return loginZed(ws, force)
		case "muse", "muse-code":
			return loginMuse(ws, force)
		default:
			return fmt.Errorf("unknown platform: %s", args[0])
		}
	},
}

func init() {
	loginCmd.Flags().BoolP("force", "f", false, "Force re-authentication even if already logged in")
}

func loginCopilot(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("copilot"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to GitHub Copilot.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	diskToken, hasDiskToken := copilot.RefreshTokenFromDisk()
	var token *oauth.Token

	switch {
	case hasDiskToken:
		fmt.Println("Found existing GitHub Copilot token on disk. Using it to authenticate...")

		t, err := copilot.RefreshToken(loginCtx, diskToken)
		if err != nil {
			return fmt.Errorf("unable to refresh token from disk: %w", err)
		}
		token = t
	default:
		fmt.Println("Requesting device code from GitHub...")
		dc, err := copilot.RequestDeviceCode(loginCtx)
		if err != nil {
			return err
		}

		clipboard.WriteText(dc.UserCode)
		fmt.Println()
		fmt.Println("The following code should be on clipboard already:")
		fmt.Println()
		lipgloss.Println(lipgloss.NewStyle().Bold(true).Render(dc.UserCode))
		fmt.Println()
		fmt.Println("Press enter to open this URL and authenticate with GitHub Copilot:")
		fmt.Println()
		lipgloss.Println(lipgloss.NewStyle().Hyperlink(dc.VerificationURI, "id=copilot").Render(dc.VerificationURI))
		fmt.Println()
		waitEnter()
		if err := browser.OpenURL(dc.VerificationURI); err != nil {
			fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
		}

		fmt.Println("Waiting for authorization...")

		t, err := copilot.PollForToken(loginCtx, dc)
		if err == copilot.ErrNotAvailable {
			fmt.Println()
			fmt.Println("GitHub Copilot is unavailable for this account. To signup, go to the following page:")
			fmt.Println()
			lipgloss.Println(lipgloss.NewStyle().Hyperlink(copilot.SignupURL, "id=copilot-signup").Render(copilot.SignupURL))
			fmt.Println()
			fmt.Println("You may be able to request free access if eligible. For more information, see:")
			fmt.Println()
			lipgloss.Println(lipgloss.NewStyle().Hyperlink(copilot.FreeURL, "id=copilot-free").Render(copilot.FreeURL))
		}
		if err != nil {
			return err
		}
		token = t
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "copilot", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with GitHub Copilot!")
	return nil
}

func loginChatGPT(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("chatgpt"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to ChatGPT.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	session, err := codex.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start ChatGPT sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with ChatGPT:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=chatgpt").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	token, err := session.Wait(loginCtx)
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "chatgpt", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with ChatGPT!")
	return nil
}

func loginAntigravity(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("antigravity"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to Antigravity.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: only Antigravity's Gemini-family models are supported; Claude and GPT-OSS models served through an Antigravity account are not.")
	fmt.Println()

	session, err := antigravity.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start Antigravity sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with your Google account:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=antigravity").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	// Bounded on top of getLoginContext's signal-only cancellation: a
	// network-level stall here (proxy, AV, DNS) would otherwise hang with
	// no feedback at all instead of failing with a clear timeout. 10
	// minutes rather than 5: project provisioning has no fixed attempt
	// cap of its own anymore (see antigravity.discoverProject) and backs
	// off up to 60s between retries while Google's onboarding backend
	// reports itself busy, so it needs real room to ride that out.
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 10*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("timed out waiting for Google sign-in to complete: %w", err)
		}
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "antigravity", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Antigravity!")
	return nil
}

func getLoginContext() context.Context {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		cancel()
		os.Exit(1)
	}()
	return ctx
}

func waitEnter() {
	_, _ = fmt.Scanln()
}

// loginClaude signs in to a Claude Pro/Max/Team subscription via
// the claude.ai console OAuth flow (internal/oauth/claude). Sign-in
// itself is real; only the model call envelope is still a stub (see
// internal/oauth/claude/oauth.go). The login/token persistence path is
// the same one the other coding plans use.
func loginClaude(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("claude"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to Claude (Pro/Max).")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: the Claude model call envelope is not yet wired up.")
	fmt.Println("You can complete sign-in, but chatting through this login will return \"not implemented\" for now.")
	fmt.Println("See internal/oauth/claude/oauth.go for the TODOs.")
	fmt.Println()

	// A machine that already runs the official Claude Code CLI has a
	// working subscription grant on disk. Reusing it skips the browser
	// consent screen entirely -- which matters because Anthropic's
	// consent screen rejects this flow for third-party callers.
	//
	// This runs even under --force: there, "re-authenticate" means
	// "pick up the current grant again", which is precisely what
	// someone does after refreshing their Claude Code login. The
	// browser flow below is the fallback when there is nothing
	// importable, not the thing --force selects.
	{
		if summary, err := claude.DescribeExisting(); err == nil {
			plan := summary.PlanType
			if plan == "" {
				plan = "unknown plan"
			}
			fmt.Printf("Found an existing Claude Code login (%s) at %s\n", plan, summary.Path)
			if summary.Expired {
				fmt.Println("Its access token has lapsed, but the refresh token renews it automatically.")
			}
			token, err := claude.ImportExisting()
			if err != nil {
				return fmt.Errorf("import existing Claude Code login: %w", err)
			}
			if err := ws.SetProviderAPIKey(config.ScopeGlobal, "claude", token); err != nil {
				return err
			}
			fmt.Println()
			fmt.Println("Imported it. You're now authenticated with Claude (Pro/Max)!")
			return nil
		} else if errors.Is(err, claude.ErrNoExistingLogin) {
			// Nothing to import; the browser flow below is the fallback.
		} else if errors.Is(err, claude.ErrExistingLoginExpired) {
			fmt.Println("An existing Claude Code login was found, but its grant has expired.")
			fmt.Println("Sign in again with the official CLI (`claude`) and re-run this command to import it.")
			fmt.Println("Continuing with browser sign-in instead.")
			fmt.Println()
		} else {
			// A corrupt or unreadable credential file is worth saying
			// out loud, but it shouldn't block the browser flow.
			fmt.Printf("Could not read the existing Claude Code login (%v); falling back to browser sign-in.\n\n", err)
		}
	}

	session, err := claude.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start Claude sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with your Claude account:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=claude").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 5*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "claude", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Claude (Pro/Max)!")
	fmt.Println("(Reminder: the model call layer is still a stub; see the docs in internal/oauth/claude.)")
	return nil
}

// loginGrokWeb signs in to an xAI SuperGrok subscription via the
// grok.com console OAuth flow (internal/oauth/grok). Same caveat as
// loginClaude: the OAuth client id and the model call envelope
// are TODOs.
func loginGrokWeb(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("grok-web"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to Grok (SuperGrok).")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: the Grok (SuperGrok) OAuth client and the model call envelope are not yet wired up.")
	fmt.Println("Until the real grok.com OAuth credentials are filled in, this login will fail at the authorize step.")
	fmt.Println("See internal/oauth/grok/oauth.go for the TODOs.")
	fmt.Println()

	session, err := grok.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start Grok sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with your xAI account:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=grok").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 5*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "grok-web", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Grok (SuperGrok)!")
	fmt.Println("(Reminder: the model call layer is still a stub; see the docs in internal/oauth/grok.)")
	return nil
}

// loginWindsurf signs in to a Windsurf Pro/Teams subscription via the
// Codeium OAuth flow (internal/oauth/windsurf). Same caveat as
// loginClaude: the OAuth client id and the model call envelope
// are TODOs.
func loginWindsurf(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("windsurf"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to Windsurf (Pro/Teams).")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: the Windsurf (Codeium) OAuth client and the model call envelope are not yet wired up.")
	fmt.Println("Until the real Windsurf/Codeium OAuth credentials are filled in, this login will fail at the authorize step.")
	fmt.Println("See internal/oauth/windsurf/oauth.go for the TODOs.")
	fmt.Println()

	session, err := windsurf.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start Windsurf sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with your Codeium/Windsurf account:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=windsurf").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 5*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "windsurf", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Windsurf (Pro/Teams)!")
	fmt.Println("(Reminder: the model call layer is still a stub; see the docs in internal/oauth/windsurf.)")
	return nil
}

// loginJetBrains signs in to a JetBrains AI Pro/Ultimate subscription
// by exchanging a JB-ACCESS-TOKEN cookie value (captured from
// account.jetbrains.com in the user's browser) for a Bearer JWT
// (internal/oauth/jetbrains). The model call envelope is a TODO.
func loginJetBrains(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("jetbrains"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to JetBrains AI (Pro/Ultimate).")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: the JetBrains JWT-exchange endpoint and the model call envelope are not yet wired up.")
	fmt.Println("Until the real exchange URL and the api.jetbrains.ai envelope are filled in, this login will fail.")
	fmt.Println("See internal/oauth/jetbrains/oauth.go for the TODOs.")
	fmt.Println()

	session, err := jetbrains.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start JetBrains sign-in: %w", err)
	}

	fmt.Println("Exchanging JB-ACCESS-TOKEN for a Bearer JWT...")
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 2*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "jetbrains", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with JetBrains AI (Pro/Ultimate)!")
	fmt.Println("(Reminder: the model call layer is still a stub; see the docs in internal/oauth/jetbrains.)")
	return nil
}

// loginAugment signs in to an Augment Code Pro/Enterprise
// subscription via the Augment OAuth flow (internal/oauth/augment).
// Same caveat as loginClaude: the OAuth client id and the model
// call envelope are TODOs.
func loginAugment(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("augment"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to Augment Code.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: the Augment Code OAuth client and the model call envelope are not yet wired up.")
	fmt.Println("Until the real Augment OAuth credentials are filled in, this login will fail at the authorize step.")
	fmt.Println("See internal/oauth/augment/oauth.go for the TODOs.")
	fmt.Println()

	session, err := augment.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start Augment sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with your Augment account:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=augment").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 5*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "augment", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Augment Code!")
	fmt.Println("(Reminder: the model call layer is still a stub; see the docs in internal/oauth/augment.)")
	return nil
}

// loginFactory signs in to a Factory AI Droids Pro/Enterprise
// subscription via the Factory OAuth flow (internal/oauth/factory).
// Same caveat: the OAuth client id and the model call envelope
// are TODOs.
func loginFactory(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("factory"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to Factory AI Droids.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: the Factory AI OAuth client and the model call envelope are not yet wired up.")
	fmt.Println("Until the real Factory OAuth credentials are filled in, this login will fail at the authorize step.")
	fmt.Println("See internal/oauth/factory/oauth.go for the TODOs.")
	fmt.Println()

	session, err := factory.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start Factory sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with your Factory account:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=factory").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 5*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "factory", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Factory AI Droids!")
	fmt.Println("(Reminder: the model call layer is still a stub; see the docs in internal/oauth/factory.)")
	return nil
}

// loginCodeRabbit signs in to a CodeRabbit Pro/Enterprise
// subscription via the CodeRabbit OAuth flow
// (internal/oauth/coderabbit). Same caveat: the OAuth client id and
// the model call envelope are TODOs.
func loginCodeRabbit(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("coderabbit"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to CodeRabbit.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: the CodeRabbit OAuth client and the model call envelope are not yet wired up.")
	fmt.Println("Until the real CodeRabbit OAuth credentials are filled in, this login will fail at the authorize step.")
	fmt.Println("See internal/oauth/coderabbit/oauth.go for the TODOs.")
	fmt.Println()

	session, err := coderabbit.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start CodeRabbit sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with your CodeRabbit account:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=coderabbit").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 5*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "coderabbit", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with CodeRabbit!")
	fmt.Println("(Reminder: the model call layer is still a stub; see the docs in internal/oauth/coderabbit.)")
	return nil
}

// loginZed signs in to a Zed Pro subscription via the Zed OAuth
// flow (internal/oauth/zed). Same caveat: the OAuth client id and
// the model call envelope are TODOs.
func loginZed(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("zed"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to Zed Pro.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	fmt.Println("Note: the Zed OAuth client and the model call envelope are not yet wired up.")
	fmt.Println("Until the real Zed OAuth credentials are filled in, this login will fail at the authorize step.")
	fmt.Println("See internal/oauth/zed/oauth.go for the TODOs.")
	fmt.Println()

	session, err := zed.Start(loginCtx)
	if err != nil {
		return fmt.Errorf("failed to start Zed sign-in: %w", err)
	}

	fmt.Println("Press enter to open your browser and sign in with your Zed account:")
	fmt.Println()
	lipgloss.Println(lipgloss.NewStyle().Hyperlink(session.AuthURL(), "id=zed").Render(session.AuthURL()))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(session.AuthURL()); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Waiting for authorization...")
	waitCtx, waitCancel := context.WithTimeout(loginCtx, 5*time.Minute)
	defer waitCancel()
	token, err := session.WaitWithProgress(waitCtx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		return err
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "zed", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Zed Pro!")
	fmt.Println("(Reminder: the model call layer is still a stub; see the docs in internal/oauth/zed.)")
	return nil
}

// loginMuse signs in to a Meta Muse Code subscription. When the
// official Muse CLI already has a session on disk it is imported;
// otherwise the Meta OIDC device-code flow runs.
func loginMuse(ws workspace.Workspace, force bool) error {
	loginCtx := getLoginContext()

	if !force {
		cfg := ws.Config()
		if cfg != nil {
			if pc, ok := cfg.Providers.Get("muse"); ok && pc.OAuthToken != nil {
				fmt.Println("You are already logged in to Meta Muse.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}
	}

	var token *oauth.Token

	if session := museoauth.SessionFromCLI(); session != nil {
		fmt.Println("Found an existing Muse Code CLI session. Importing it...")
		t, err := museoauth.TokenFromSession(loginCtx, session)
		if err != nil {
			return fmt.Errorf("unable to use the Muse CLI session: %w", err)
		}
		token = t
	} else {
		fmt.Println("Requesting device code from Meta...")
		dc, err := museoauth.RequestDeviceCode(loginCtx)
		if err != nil {
			return err
		}

		clipboard.WriteText(dc.UserCode)
		fmt.Println()
		fmt.Println("The following code should be on clipboard already:")
		fmt.Println()
		lipgloss.Println(lipgloss.NewStyle().Bold(true).Render(dc.UserCode))
		fmt.Println()
		fmt.Println("Press enter to open this URL and authenticate with your Meta account:")
		fmt.Println()
		verifyURL := dc.VerificationURL()
		lipgloss.Println(lipgloss.NewStyle().Hyperlink(verifyURL, "id=muse").Render(verifyURL))
		fmt.Println()
		waitEnter()
		if err := browser.OpenURL(verifyURL); err != nil {
			fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
		}

		fmt.Println("Waiting for authorization...")

		t, err := museoauth.PollForToken(loginCtx, dc)
		if err != nil {
			return err
		}
		token = t
	}

	if err := ws.SetProviderAPIKey(config.ScopeGlobal, "muse", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Meta Muse!")
	return nil
}
