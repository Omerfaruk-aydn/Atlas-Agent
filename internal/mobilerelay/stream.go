package mobilerelay

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// streamConn adapts bounded binary WebSocket messages to a single HTTP
// connection. Neither side can choose a network destination through it.
type streamConn struct {
	*websocket.Conn
	reader io.Reader
	write  sync.Mutex
	once   sync.Once
	done   chan struct{}
}

func newStream(conn *websocket.Conn) *streamConn {
	conn.SetReadLimit(64 << 10)
	return &streamConn{Conn: conn, done: make(chan struct{})}
}

func (c *streamConn) Read(b []byte) (int, error) {
	for {
		if c.reader == nil {
			kind, reader, err := c.NextReader()
			if err != nil {
				return 0, err
			}
			if kind != websocket.BinaryMessage {
				return 0, io.ErrUnexpectedEOF
			}
			c.reader = reader
		}
		n, err := c.reader.Read(b)
		if err == io.EOF {
			c.reader = nil
			if n == 0 {
				continue
			}
			return n, nil
		}
		return n, err
	}
}

func (c *streamConn) Write(b []byte) (int, error) {
	c.write.Lock()
	defer c.write.Unlock()
	written := 0
	for len(b) > 0 {
		n := min(len(b), 32<<10)
		if err := c.WriteMessage(websocket.BinaryMessage, b[:n]); err != nil {
			return written, err
		}
		written += n
		b = b[n:]
	}
	return written, nil
}

func (c *streamConn) Close() error {
	var err error
	c.once.Do(func() {
		close(c.done)
		err = c.Conn.Close()
	})
	return err
}

func (c *streamConn) LocalAddr() net.Addr  { return c.UnderlyingConn().LocalAddr() }
func (c *streamConn) RemoteAddr() net.Addr { return c.UnderlyingConn().RemoteAddr() }
func (c *streamConn) SetDeadline(t time.Time) error {
	return c.UnderlyingConn().SetDeadline(t)
}

func (c *streamConn) SetReadDeadline(t time.Time) error {
	return c.UnderlyingConn().SetReadDeadline(t)
}

func (c *streamConn) SetWriteDeadline(t time.Time) error {
	return c.UnderlyingConn().SetWriteDeadline(t)
}

// oneListener serves one virtual connection, then exits when it closes.
type oneListener struct {
	conn  *streamConn
	taken bool
}

func (l *oneListener) Accept() (net.Conn, error) {
	if !l.taken {
		l.taken = true
		return l.conn, nil
	}
	<-l.conn.done
	return nil, net.ErrClosed
}

func (l *oneListener) Close() error   { return l.conn.Close() }
func (l *oneListener) Addr() net.Addr { return l.conn.LocalAddr() }
