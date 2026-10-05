package activity

import (
	"bufio"
	"io"
	"time"
)

// watchCursorOwner requires heartbeat traffic while cursor ownership is held.
func watchCursorOwner(input io.Reader, output io.Writer, timeout time.Duration, restore func()) {
	_, _ = output.Write([]byte{'R'})
	events := make(chan byte, 1)
	go func() {
		defer close(events)
		reader := bufio.NewReader(input)
		for {
			b, err := reader.ReadByte()
			if err != nil {
				return
			}
			events <- b
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case b, ok := <-events:
			if ok && b == 'Q' {
				return
			}
			if !ok {
				restore()
				return
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		case <-timer.C:
			restore()
			return
		}
	}
}
