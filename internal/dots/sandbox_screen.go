package dots

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/isolation"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

const (
	IsolationSandbox = "windows_sandbox"
	SandboxNote      = "Bu, ajanın Windows'u. Görev çubuğundaki Sandbox penceresiyle aynı görüntü. Pencereyi küçültünce görüntü boşalır."
	SandboxStarting  = "Sandbox açılıyor. Pencere birazdan masaüstünde belirir; görüntü buraya da düşer."
)

func sandboxManager() *isolation.Manager {
	exe, _ := os.Executable()
	return isolation.NewManager(config.GlobalDevicesDir(), exe, nil)
}

func SandboxCapable(ctx context.Context) bool {
	for _, c := range sandboxManager().Capabilities(ctx) {
		if c.Backend == isolation.BackendWindowsSandbox && c.Available {
			return true
		}
	}
	active, _ := sandboxManager().Active()
	return active != nil && active.Alive
}

var sandboxBoot struct {
	mu       sync.Mutex
	starting bool
	err      string
}

// BootSandbox starts the agent's Windows when the machine can run one.
// The call returns immediately; the window and the picture appear when
// the guest agent is up.
func BootSandbox(ctx context.Context) {
	if !SandboxCapable(ctx) {
		return
	}
	if active, _ := sandboxManager().Active(); active != nil && active.Alive {
		return
	}
	sandboxBoot.mu.Lock()
	if sandboxBoot.starting {
		sandboxBoot.mu.Unlock()
		return
	}
	sandboxBoot.starting = true
	sandboxBoot.err = ""
	sandboxBoot.mu.Unlock()
	go func() {
		defer func() {
			sandboxBoot.mu.Lock()
			sandboxBoot.starting = false
			sandboxBoot.mu.Unlock()
		}()
		bootCtx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		dir, profile := isolation.DefaultChrome()
		opts := isolation.StartOptions{MemoryMB: 2048, ChromeDir: dir, ChromeProfile: profile}
		mgr := sandboxManager()
		st, err := mgr.Start(bootCtx, opts)
		sandboxBoot.mu.Lock()
		if err != nil {
			sandboxBoot.err = err.Error()
		}
		sandboxBoot.mu.Unlock()
		if err == nil {
			_, _ = mgr.Pause(st.Lease.ID, "pencere önde, ekran sekmesi aynı görüntüyü basıyor")
			_, _ = mgr.Resume(st.Lease.ID)
		}
	}()
}

func sandboxState() (alive, starting bool, errText string) {
	active, err := sandboxManager().Active()
	if err == nil && active != nil && active.Alive {
		alive = true
	}
	sandboxBoot.mu.Lock()
	starting = sandboxBoot.starting
	errText = sandboxBoot.err
	sandboxBoot.mu.Unlock()
	return alive, starting, errText
}

func sandboxPNG(ctx context.Context) ([]byte, error) {
	reg := nodes.OpenRegistry(config.GlobalDevicesDir())
	entry, token, err := reg.Get(isolation.SandboxNodeName)
	if err != nil {
		return nil, err
	}
	client, err := nodes.NewClient(entry.URL, entry.Fingerprint, token)
	if err != nil {
		return nil, err
	}
	obs, err := client.Preview(ctx)
	if err != nil {
		return nil, err
	}
	if len(obs.PNG) == 0 {
		return nil, ErrScreenOff
	}
	return obs.PNG, nil
}
