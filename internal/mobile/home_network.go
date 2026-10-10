package mobile

import (
	"cmp"
	"net"
	"net/netip"
	"slices"
)

// HomeNetworkAddress identifies a private IPv4 address and its interface.
type HomeNetworkAddress struct {
	Interface string
	Address   string
}

// HomeNetworkAddresses discovers active LAN interfaces without contacting
// an external service. VPN and virtual interfaces are shown if private;
// callers must let the user choose when more than one address exists.
func HomeNetworkAddresses() ([]HomeNetworkAddress, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addresses []HomeNetworkAddress
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			prefix, err := netip.ParsePrefix(addr.String())
			if err != nil || !IsHomeNetworkAddress(prefix.Addr().String()) {
				continue
			}
			addresses = append(addresses, HomeNetworkAddress{Interface: iface.Name, Address: prefix.Addr().String()})
		}
	}
	slices.SortFunc(addresses, func(a, b HomeNetworkAddress) int {
		if n := cmp.Compare(a.Interface, b.Interface); n != 0 {
			return n
		}
		return cmp.Compare(a.Address, b.Address)
	})
	return slices.Compact(addresses), nil
}

// IsHomeNetworkAddress accepts only private IPv4 LAN addresses. Public,
// loopback, link-local and CGNAT addresses are outside home testing mode.
func IsHomeNetworkAddress(address string) bool {
	ip, err := netip.ParseAddr(address)
	return err == nil && ip.Is4() && ip.IsPrivate()
}
