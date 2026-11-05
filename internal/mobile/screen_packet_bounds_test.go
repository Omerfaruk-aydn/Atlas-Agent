package mobile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScreenPacketRejectsOversizedPayload(t *testing.T) {
	t.Parallel()
	packet := &screenPacket{payload: make([]byte, screenMaxFrame+1)}
	require.Nil(t, packet.encode())
	packet.payload = packet.payload[:screenMaxFrame]
	require.Len(t, packet.encode(), screenHeaderSize+screenMaxFrame)
}
