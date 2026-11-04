package mailbox

import (
	"bufio"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecentReadsCodesWithoutEchoingTheSecret(t *testing.T) {
	t.Parallel()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	go func() {
		defer server.Close()
		r := bufio.NewReader(server)
		_, _ = server.Write([]byte("* OK ready\r\n"))
		_, _ = r.ReadString('\n')
		_, _ = server.Write([]byte("A1 BAD AUTHENTICATE unknown\r\n"))
		_, _ = r.ReadString('\n')
		_, _ = server.Write([]byte("A2 OK\r\n"))
		_, _ = r.ReadString('\n')
		_, _ = server.Write([]byte("* 1 EXISTS\r\nA3 OK [READ-ONLY]\r\n"))
		_, _ = r.ReadString('\n')
		body := "From: Instagram <security@instagram.com>\r\nSubject: Giriş kodun\r\nDate: Tue, 4 Nov 2026 11:00:00 +0000\r\n\r\nkodun 482193\r\n"
		_, _ = server.Write([]byte("* 1 FETCH (BODY[HEADER.FIELDS (FROM SUBJECT DATE)] {" + strconv.Itoa(len(body)) + "}\r\n" + body + " BODY[TEXT]<0> {0}\r\n)\r\nA4 OK\r\n"))
	}()
	messages, err := readRecent(client, "owner@gmail.com", "secret-value", 5)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, []string{"482193"}, messages[0].Codes)
	require.NotContains(t, messages[0].Excerpt, "secret-value")
	require.Contains(t, messages[0].Subject, "Giriş kodun")
}

func TestCodesPreferHintedNumbers(t *testing.T) {
	t.Parallel()
	got := codes("sipariş 123456 ve doğrulama kodu 908172")
	require.Equal(t, []string{"908172"}, got)
}
