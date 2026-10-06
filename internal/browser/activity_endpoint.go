package browser

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// A loopback endpoint is only a candidate for native presentation. Windows
// additionally verifies that the listening socket belongs to the CDP PID.
func localBrowserEndpointPort(raw string) (uint16, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return 0, false
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return 0, false
	}
	port := u.Port()
	switch u.Scheme {
	case "http", "ws":
		if port == "" {
			port = "80"
		}
	case "https", "wss":
		if port == "" {
			port = "443"
		}
	default:
		return 0, false
	}
	value, err := strconv.ParseUint(port, 10, 16)
	return uint16(value), err == nil && value != 0
}
