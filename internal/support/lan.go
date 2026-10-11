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

