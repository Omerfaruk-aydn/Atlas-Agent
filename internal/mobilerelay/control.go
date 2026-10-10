package mobilerelay

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type invitation struct {
	ID string `json:"id"`
}

type control struct {
	conn  *websocket.Conn
	write sync.Mutex
	done  chan struct{}
	once  sync.Once
}

func newControl(conn *websocket.Conn) *control {
	conn.SetReadLimit(4096)
	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	})
	return &control{conn: conn, done: make(chan struct{})}
}

func (c *control) send(v invitation) error {
	c.write.Lock()
	defer c.write.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteJSON(v)
}

func (c *control) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *control) heartbeat(ctx context.Context) {
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			c.close()
			return
		case <-c.done:
			return
		case <-tick.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				c.close()
				return
			}
		}
	}
}
