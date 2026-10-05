package terminal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPTYChildFixture(t *testing.T) {
	if os.Getenv("ATLAS_PTY_TEST_CHILD") != "1" {
		return
	}
	if !fixtureIsTerminal() {
		fmt.Println("PTY_INPUT_UNAVAILABLE")
		os.Exit(81)
	}
	if os.Getenv("ATLAS_PTY_TEST_CASE") == "overflow" {
		for range 1024 {
			fmt.Print(strings.Repeat("x", 32*1024))
		}
		os.Exit(0)
	}
	fmt.Println("PTY_READY")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "size":
			size, err := fixtureTerminalSize()
			if err != nil {
				os.Exit(82)
			}
			fmt.Printf("SIZE=%dx%d\n", size.Width, size.Height)
		case "quit":
			fmt.Println("PTY_EXIT")
			os.Exit(0)
		case "scroll":
			for i := range 100 {
				fmt.Printf("SCROLL_%03d\n", i)
			}
		default:
			fmt.Printf("INPUT=%s\n", scanner.Text())
		}
	}
	os.Exit(83)
}

func TestPTYOutputLimitAndDeadlineStopOwnedTerminal(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"overflow", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			completionTimeout := 15 * time.Second
			if mode == "overflow" {
				// ConPTY renders over 16 MiB before the transcript limit fires.
				// Race instrumentation and shared CI CPUs slow that work down.
				completionTimeout = time.Minute
			}
			ctx, cancel := context.WithTimeout(t.Context(), completionTimeout)
			defer cancel()
			req := execution.Request{Root: t.TempDir(), Argv: []string{exe, "-test.run=^TestPTYChildFixture$"}, Env: append(os.Environ(), "ATLAS_PTY_TEST_CHILD=1", "ATLAS_PTY_TEST_CASE="+mode)}
			if mode == "deadline" {
				req.Policy.TimeoutMS = 100
			}
			session, err := Start(ctx, req, execution.TerminalSize{Width: 80, Height: 24})
			if errors.Is(err, ErrUnavailable) {
				t.Skip("Platform has no real terminal backend; no terminal proof was produced")
			}
			require.NoError(t, err)
			defer session.Close()
			result, err := session.Wait(ctx)
			if mode == "overflow" {
				require.Error(t, err)
				require.Equal(t, "output_limit", result.Status)
			} else {
				require.NoError(t, err)
				require.Equal(t, "cancelled", result.Status)
			}
			require.True(t, result.Done)
			require.NotNil(t, result.ExitCode)
			data, err := io.ReadAll(session)
			require.NoError(t, err)
			require.LessOrEqual(t, len(data), MaxTranscriptBytes)
		})
	}
}

func TestPTYRealInputResizeCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	executable, err := os.Executable()
	require.NoError(t, err)
	request := execution.Request{Root: t.TempDir(), Argv: []string{executable, "-test.run=^TestPTYChildFixture$"}, Env: append(os.Environ(), "ATLAS_PTY_TEST_CHILD=1")}
	session, err := Start(ctx, request, execution.TerminalSize{Width: 80, Height: 24})
	if errors.Is(err, ErrUnavailable) {
		t.Skip("Platform has no real terminal backend; no terminal proof was produced")
	}
	require.NoError(t, err, "a missing real PTY is not a passing terminal test")
	defer session.Close()
	require.NotEmpty(t, session.Identity().RunID)
	var mu sync.Mutex
	var transcript strings.Builder
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		buffer := make([]byte, 4096)
		for {
			n, err := session.Read(buffer)
			mu.Lock()
			transcript.Write(buffer[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	contains := func(text string) bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(transcript.String(), text)
	}
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		mu.Lock()
		text := transcript.String()
		mu.Unlock()
		require.Contains(check, text, "PTY_READY")
		probe, stop := context.WithTimeout(ctx, time.Millisecond)
		defer stop()
		result, _ := session.Wait(probe)
		if result.Done {
			require.Fail(check, fmt.Sprintf("PTY exited before ready: %+v", result))
		}
	}, 5*time.Second, 20*time.Millisecond)
	_, err = io.WriteString(session, "hello\r")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return contains("INPUT=hello") }, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, session.Resize(ctx, execution.TerminalSize{Width: 120, Height: 40}))
	_, err = io.WriteString(session, "size\rscroll\r")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return contains("SIZE=120x40") && contains("SCROLL_099") }, 3*time.Second, 20*time.Millisecond)
	_, err = io.WriteString(session, "quit\r")
	require.NoError(t, err)
	result, err := session.Wait(ctx)
	require.NoError(t, err)
	require.True(t, result.Done)
	require.NotNil(t, result.ExitCode)
	require.Zero(t, *result.ExitCode)
	select {
	case <-drainDone:
	case <-ctx.Done():
		t.Fatal("PTY output did not finish")
	}
	require.True(t, contains("PTY_EXIT"))

	second, err := Start(ctx, request, execution.TerminalSize{Width: 40, Height: 12})
	require.NoError(t, err)
	require.NoError(t, second.Close())
	result, err = second.Wait(ctx)
	require.NoError(t, err)
	require.True(t, result.Done)
	require.Equal(t, "cancelled", result.Status)
	_, err = Start(ctx, execution.Request{Root: request.Root, Argv: request.Argv, Policy: execution.ExecutionPolicy{Mode: "container-required"}}, execution.TerminalSize{Width: 80, Height: 24})
	require.ErrorIs(t, err, ErrUnavailable)
}
