package mobile

import (
	"bufio"
	"net"
	"sync"
	"time"
)

// protocolListener shares one TCP port between TLS and a redirect-only
// HTTP server. Classification is bounded and runs outside Accept, so an
// idle or slow client cannot block other phones from connecting.
type protocolListener struct {
	listener net.Listener
	done     chan struct{}
	tls      chan net.Conn
	plain    chan net.Conn
	slots    chan struct{}
	once     sync.Once
	mu       sync.Mutex
	pending  map[net.Conn]struct{}
}

type protocolBranch struct {
	owner *protocolListener
	ready chan net.Conn
}

type bufferedConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConnection) Read(p []byte) (int, error) { return c.reader.Read(p) }

func splitProtocols(listener net.Listener) (*protocolBranch, *protocolBranch) {
	p := &protocolListener{
		listener: listener, done: make(chan struct{}),
		tls: make(chan net.Conn), plain: make(chan net.Conn),
		slots: make(chan struct{}, 64), pending: make(map[net.Conn]struct{}),
	}
	go p.dispatch()
	return &protocolBranch{owner: p, ready: p.tls}, &protocolBranch{owner: p, ready: p.plain}
}

func (p *protocolListener) dispatch() {
	defer p.Close() //nolint:errcheck
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		select {
		case p.slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		p.mu.Lock()
		select {
		case <-p.done:
			p.mu.Unlock()
			_ = conn.Close()
			<-p.slots
			return
		default:
			p.pending[conn] = struct{}{}
			p.mu.Unlock()
		}
		go p.classify(conn)
	}
}

func (p *protocolListener) classify(conn net.Conn) {
	defer func() {
		p.mu.Lock()
		delete(p.pending, conn)
		p.mu.Unlock()
		<-p.slots
	}()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(conn)
	first, err := reader.Peek(1)
	if err != nil {
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	ready := p.plain
	if first[0] == 0x16 {
		// TLS clients start with a handshake record. ServeTLS validates the
		// remaining handshake and the configured minimum TLS version.
		ready = p.tls
	}
	wrapped := &bufferedConnection{Conn: conn, reader: reader}
	select {
	case ready <- wrapped:
	case <-p.done:
		_ = conn.Close()
	}
}

func (p *protocolListener) Close() error {
	var err error
	p.once.Do(func() {
		close(p.done)
		err = p.listener.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		for conn := range p.pending {
			_ = conn.Close()
		}
	})
	return err
}

func (b *protocolBranch) Accept() (net.Conn, error) {
	select {
	case conn := <-b.ready:
		return conn, nil
	case <-b.owner.done:
		return nil, net.ErrClosed
	}
}

func (b *protocolBranch) Close() error   { return b.owner.Close() }
func (b *protocolBranch) Addr() net.Addr { return b.owner.listener.Addr() }
