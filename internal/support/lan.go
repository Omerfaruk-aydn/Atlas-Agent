package support

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"
)

// DefaultDiscoveryPort is the UDP port hosts answer discovery queries on.
const DefaultDiscoveryPort = 47653

type discoveryMsg struct {
	V      int    `json:"v"`
	Slot   string `json:"slot"`
	Nonce  string `json:"nonce"`
	Port   int    `json:"port,omitempty"`
	NodeID string `json:"node_id,omitempty"`
}

// discoveryResponder answers queries for the slots the host currently
// offers. It reveals only the TCP port and node id, and only to a querier
// that already knows the slot.
type discoveryResponder struct {
	conn   *net.UDPConn
	port   int
	nodeID string
	slots  func() []string
	mu     sync.Mutex
	sent   []time.Time
	closed chan struct{}
}

func startDiscovery(port, tcpPort int, nodeID string, slots func() []string) (*discoveryResponder, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return nil, err
	}
	d := &discoveryResponder{conn: conn, port: tcpPort, nodeID: nodeID, slots: slots, closed: make(chan struct{})}
	go d.loop()
	return d, nil
}

func (d *discoveryResponder) allow() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := time.Now().Add(-time.Second)
	kept := d.sent[:0]
	for _, t := range d.sent {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	d.sent = kept
	if len(kept) >= 20 {
		return false
	}
	d.sent = append(d.sent, time.Now())
	return true
}

func (d *discoveryResponder) loop() {
	buf := make([]byte, 512)
	for {
		n, from, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-d.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		var q discoveryMsg
		if json.Unmarshal(buf[:n], &q) != nil || q.V != 1 || !slotPattern.MatchString(q.Slot) || len(q.Nonce) > 64 {
			continue
		}
		if !slices.Contains(d.slots(), q.Slot) || !d.allow() {
			continue
		}
		reply, _ := json.Marshal(discoveryMsg{V: 1, Slot: q.Slot, Nonce: q.Nonce, Port: d.port, NodeID: d.nodeID})
		_, _ = d.conn.WriteToUDP(reply, from)
	}
}

func (d *discoveryResponder) Close() {
	close(d.closed)
	_ = d.conn.Close()
}

// discover asks the local network which host offers slot and returns
// host:port addresses that answered, in answer order.
func discover(ctx context.Context, slot string, port int, wait time.Duration) ([]string, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	nonce := randomID(8)
	query, _ := json.Marshal(discoveryMsg{V: 1, Slot: slot, Nonce: nonce})
	targets := []net.IP{net.IPv4bcast, net.IPv4(127, 0, 0, 1)}
	targets = append(targets, broadcastAddrs()...)
	send := func() {
		for _, ip := range targets {
			_, _ = conn.WriteToUDP(query, &net.UDPAddr{IP: ip, Port: port})
		}
	}
	send()
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	resend := time.Now().Add(wait / 3)
	var found []string
	buf := make([]byte, 512)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return found, ctx.Err()
		}
		next := time.Now().Add(150 * time.Millisecond)
		if next.After(deadline) {
			next = deadline
		}
		_ = conn.SetReadDeadline(next)
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if time.Now().After(resend) {
				send()
				resend = deadline
			}
			continue
		}
		var reply discoveryMsg
		if json.Unmarshal(buf[:n], &reply) != nil || reply.Nonce != nonce || reply.Slot != slot || reply.Port <= 0 || reply.Port > 65535 {
			continue
		}
		addr := net.JoinHostPort(from.IP.String(), strconv.Itoa(reply.Port))
		if !slices.Contains(found, addr) {
			found = append(found, addr)
		}
		// One answer is enough; a short grace period catches a loopback
		// answer that arrives with a LAN one.
		if len(found) == 1 {
			if grace := time.Now().Add(200 * time.Millisecond); grace.Before(deadline) {
				deadline = grace
			}
		}
	}
	return found, nil
}

// broadcastAddrs returns directed broadcast addresses of up IPv4
// interfaces, which reach hosts when 255.255.255.255 is filtered.
func broadcastAddrs() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.To4()
			mask := ipnet.Mask
			if len(mask) == net.IPv6len {
				mask = mask[12:]
			}
			b := make(net.IP, 4)
			for i := range 4 {
				b[i] = ip[i] | ^mask[i]
			}
			out = append(out, b)
		}
	}
	return out
}

// LocalAddresses lists private IPv4 addresses for display.
func LocalAddresses() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && ipnet.IP.IsPrivate() {
				out = append(out, ipnet.IP.String())
			}
		}
	}
	return out
}
