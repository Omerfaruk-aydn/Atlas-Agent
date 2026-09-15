package browser

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeSession is a Session double that records calls instead of driving a
// real browser, so Manager's reuse/reap/close bookkeeping can be tested
// without launching Chrome.
type fakeSession struct {
	closed bool
	navs   []string
}

func (f *fakeSession) Navigate(url string) error { f.navs = append(f.navs, url); return nil }
func (f *fakeSession) Click(string) error        { return nil }
func (f *fakeSession) Type(string, string) error { return nil }
func (f *fakeSession) PressKey(name string) error {
	if _, ok := ResolveKey(name); !ok {
		return fmt.Errorf("unsupported key %q", name)
	}
	return nil
}
func (f *fakeSession) Eval(string) (string, error)     { return "", nil }
func (f *fakeSession) Text(string) (string, error)     { return "", nil }
func (f *fakeSession) HTML(string) (string, error)     { return "", nil }
func (f *fakeSession) Screenshot(bool) ([]byte, error) { return []byte("png"), nil }
func (f *fakeSession) AnnotatedScreenshot(bool) ([]byte, error) {
	return []byte("png"), nil
}
func (f *fakeSession) ClickAt(float64, float64) error { return nil }

func (f *fakeSession) URL() (string, error)                                  { return "https://example.com", nil }
func (f *fakeSession) Back() error                                           { return nil }
func (f *fakeSession) Forward() error                                        { return nil }
func (f *fakeSession) Scroll(int, int) error                                 { return nil }
func (f *fakeSession) Snapshot(bool) ([]SnapshotElement, error)              { return nil, nil }
func (f *fakeSession) Images() ([]ImageInfo, error)                          { return nil, nil }
func (f *fakeSession) ConsoleLogs() []ConsoleEntry                           { return nil }
func (f *fakeSession) PendingDialogs() []DialogInfo                          { return nil }
func (f *fakeSession) HandleDialog(bool, string) error                       { return nil }
func (f *fakeSession) RawCDP(string, map[string]any) (map[string]any, error) { return nil, nil }
func (f *fakeSession) Close()                                                { f.closed = true }

func newTestManager() (*Manager, *int) {
	created := 0
	m := newManager(Options{}, func(Options) (Session, error) {
		created++
		return &fakeSession{}, nil
	})
	return m, &created
}

func TestManagerReusesSessionForTheSameID(t *testing.T) {
	t.Parallel()
	m, created := newTestManager()

	s1, err := m.Session("sess-1")
	require.NoError(t, err)
	s2, err := m.Session("sess-1")
	require.NoError(t, err)

	require.Same(t, s1, s2)
	require.Equal(t, 1, *created)
}

func TestManagerLaunchesSeparateSessionsPerID(t *testing.T) {
	t.Parallel()
	m, created := newTestManager()

	_, err := m.Session("sess-1")
	require.NoError(t, err)
	_, err = m.Session("sess-2")
	require.NoError(t, err)

	require.Equal(t, 2, *created)
}

func TestManagerClosePropagatesToTheSession(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager()

	s, err := m.Session("sess-1")
	require.NoError(t, err)
	fake := s.(*fakeSession)

	m.Close("sess-1")
	require.True(t, fake.closed)

	// A closed session is dropped, not reused.
	s2, err := m.Session("sess-1")
	require.NoError(t, err)
	require.NotSame(t, s, s2)
}

func TestManagerCloseOnUnknownIDIsANoop(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager()
	m.Close("never-opened") // must not panic
}

func TestManagerCloseAllClosesEverySession(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager()

	s1, err := m.Session("sess-1")
	require.NoError(t, err)
	s2, err := m.Session("sess-2")
	require.NoError(t, err)

	m.CloseAll()

	require.True(t, s1.(*fakeSession).closed)
	require.True(t, s2.(*fakeSession).closed)

	s3, err := m.Session("sess-1")
	require.NoError(t, err)
	require.NotSame(t, s1, s3)
}

func TestManagerReapsSessionsIdleLongerThanIdleTimeout(t *testing.T) {
	t.Parallel()
	created := 0
	m := newManager(Options{IdleTimeout: time.Millisecond}, func(Options) (Session, error) {
		created++
		return &fakeSession{}, nil
	})

	s1, err := m.Session("sess-1")
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)

	s2, err := m.Session("sess-1")
	require.NoError(t, err)
	require.NotSame(t, s1, s2, "an idle session past IdleTimeout should be relaunched, not reused")
	require.True(t, s1.(*fakeSession).closed, "the reaped session should be closed")
	require.Equal(t, 2, created)
}

func TestManagerNeverReapsWhenIdleTimeoutIsZero(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager() // Options{} -> IdleTimeout: 0

	s1, err := m.Session("sess-1")
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)

	s2, err := m.Session("sess-1")
	require.NoError(t, err)
	require.Same(t, s1, s2)
}

func TestManagerPropagatesSessionCreationErrors(t *testing.T) {
	t.Parallel()
	wantErr := fmt.Errorf("no chrome binary found")
	m := newManager(Options{}, func(Options) (Session, error) {
		return nil, wantErr
	})

	_, err := m.Session("sess-1")
	require.ErrorIs(t, err, wantErr)
}

func TestResolveKeyIsCaseSensitiveOnKnownNames(t *testing.T) {
	t.Parallel()

	for _, name := range SupportedKeys() {
		_, ok := ResolveKey(name)
		require.True(t, ok, "SupportedKeys() returned %q but ResolveKey rejected it", name)
	}

	_, ok := ResolveKey("not-a-real-key")
	require.False(t, ok)
}
