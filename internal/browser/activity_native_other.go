//go:build !windows

package browser

import "github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"

func newNativeBrowserActivity(*chromedpSession) activity.Renderer { return nil }
