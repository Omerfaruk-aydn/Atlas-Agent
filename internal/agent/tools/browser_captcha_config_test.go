package tools

import (
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/captcha"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCaptchaChainConcurrentConfiguration(t *testing.T) {
	previous := currentCaptchaChain()
	t.Cleanup(func() { SetCaptchaChain(previous) })
	chain := captcha.Build(captcha.Options{Enabled: false})
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 100 {
				SetCaptchaChain(chain)
				_ = currentCaptchaChain()
			}
		})
	}
	workers.Wait()
	require.Same(t, chain, currentCaptchaChain())
}

func TestInitCaptchaChainFromAtlasJSON(t *testing.T) {
	// A raw key written straight into atlas.json must reach the chain
	// without any environment variable set.
	t.Setenv("ATLAS_CAPTCHA_API_KEY", "")
	t.Setenv("ATLAS_CAPTCHA_PROVIDER", "")

	prev := currentCaptchaChain()
	t.Cleanup(func() { SetCaptchaChain(prev) })

	InitCaptchaChain(config.ToolCaptcha{
		Enabled:        boolPtr(true),
		RemoteProvider: "2captcha",
		RemoteAPIKey:   "raw-key-from-atlas-json",
	})
	require.NotNil(t, currentCaptchaChain())
	require.True(t, currentCaptchaChain().Supports(captcha.KindRecaptchaV2), "remote solver must be registered from atlas.json key")
	require.True(t, currentCaptchaChain().Supports(captcha.KindMath), "math solver is always available")
	require.Contains(t, currentCaptchaChain().Solvers(), "math")
}

func TestInitCaptchaChainEnvRefExpansion(t *testing.T) {
	t.Setenv("MY_CAPTCHA_KEY", "expanded-remote-key")
	t.Setenv("ATLAS_CAPTCHA_API_KEY", "")

	prev := currentCaptchaChain()
	t.Cleanup(func() { SetCaptchaChain(prev) })

	InitCaptchaChain(config.ToolCaptcha{
		Enabled:        boolPtr(true),
		RemoteProvider: "capsolver",
		RemoteAPIKey:   "$MY_CAPTCHA_KEY",
	})
	require.NotNil(t, currentCaptchaChain())
	require.True(t, currentCaptchaChain().Supports(captcha.KindRecaptchaV2))
}

func TestInitCaptchaChainEnvFallbackWhenFieldEmpty(t *testing.T) {
	t.Setenv("ATLAS_CAPTCHA_API_KEY", "env-fallback-key")
	t.Setenv("ATLAS_CAPTCHA_PROVIDER", "anticaptcha")

	prev := currentCaptchaChain()
	t.Cleanup(func() { SetCaptchaChain(prev) })

	InitCaptchaChain(config.ToolCaptcha{Enabled: boolPtr(true)})
	require.NotNil(t, currentCaptchaChain())
	require.True(t, currentCaptchaChain().Supports(captcha.KindRecaptchaV2),
		"empty atlas.json key must fall back to ATLAS_CAPTCHA_API_KEY + ATLAS_CAPTCHA_PROVIDER")
}

func TestInitCaptchaChainOfflineOnly(t *testing.T) {
	// No remote key at all: the chain must still work offline for math,
	// and image-text falls through to the model-vision path.
	t.Setenv("ATLAS_CAPTCHA_API_KEY", "")
	t.Setenv("ATLAS_CAPTCHA_PROVIDER", "")

	prev := currentCaptchaChain()
	t.Cleanup(func() { SetCaptchaChain(prev) })

	InitCaptchaChain(config.ToolCaptcha{Enabled: boolPtr(true)})
	require.NotNil(t, currentCaptchaChain())
	require.True(t, currentCaptchaChain().Supports(captcha.KindMath))
	require.False(t, currentCaptchaChain().Supports(captcha.KindRecaptchaV2), "no key means no remote token solver")
}

func TestInitCaptchaChainDisabled(t *testing.T) {
	prev := currentCaptchaChain()
	t.Cleanup(func() { SetCaptchaChain(prev) })

	InitCaptchaChain(config.ToolCaptcha{Enabled: boolPtr(false)})
	require.NotNil(t, currentCaptchaChain())
	require.Empty(t, currentCaptchaChain().Solvers())
	require.False(t, currentCaptchaChain().Supports(captcha.KindMath))
}

func TestExpandCaptchaSecret(t *testing.T) {
	t.Setenv("SOME_KEY", "resolved-value")
	require.Equal(t, "plain-key", expandCaptchaSecret("plain-key"))
	require.Equal(t, "", expandCaptchaSecret(""))
	require.Equal(t, "resolved-value", expandCaptchaSecret("$SOME_KEY"))
	require.Equal(t, "resolved-value", expandCaptchaSecret("${SOME_KEY}"))
	require.Equal(t, "x-resolved-value-y", expandCaptchaSecret("x-${SOME_KEY}-y"))
	require.Equal(t, "", expandCaptchaSecret("$NO_SUCH_VAR_AT_ALL"))
}

func boolPtr(b bool) *bool { return &b }
