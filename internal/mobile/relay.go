package mobile

import (
	"fmt"
	"net/url"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
)

func relayConfiguration(opts Options) (*url.URL, string, error) {
	if opts.RelayURL == "" && opts.RelayTokenFile == "" {
		return nil, "", nil
	}
	if opts.RelayURL == "" || opts.RelayTokenFile == "" {
		return nil, "", fmt.Errorf("mobile relay URL and private key file must be configured together")
	}
	origin, err := mobilerelay.Origin(opts.RelayURL)
	if err != nil {
		return nil, "", err
	}
	token, err := mobilerelay.ReadToken(opts.RelayTokenFile)
	return origin, token, err
}
