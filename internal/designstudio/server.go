package designstudio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StudioPage is the editor's standalone page in the renderer build.
const StudioPage = "design.html"

// Workspace is the id the studio page uses in API paths; the studio serves
// one project, so it is fixed.
const Workspace = "studio"

// Server serves the design editor for one project on a loopback port, for
// a browser the agent drives. It is the same editor as the desktop app's
// Design tab, working on the same files, so the person sees the agent's
// edits live there.
type Server struct {
	Root  string // the project
	Dir   string // the renderer build
	base  string
	token string
	srv   *http.Server
}

var (
	mu      sync.Mutex
	servers = map[string]*Server{}
)

// Open starts the studio for root, or returns the one already running.
func Open(root string) (*Server, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	if s, ok := servers[abs]; ok {
		return s, nil
	}
	dir, err := RendererDir()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	s := &Server{Root: abs, Dir: dir, base: "http://" + ln.Addr().String(), token: hex.EncodeToString(buf)}
	s.srv = &http.Server{Handler: s.handler(ln.Addr().String()), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	servers[abs] = s
	return s, nil
}

// Close stops every studio server; tests use it.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	for k, s := range servers {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = s.srv.Shutdown(ctx)
		cancel()
		delete(servers, k)
	}
}

// URL is the editor's address with file open; an empty file opens the
// last one.
func (s *Server) URL(file string) string {
	u := s.base + "/" + StudioPage
	if file != "" {
		u += "?file=" + url.QueryEscape(file)
	}
	return u
}

// Base is the server's origin.
func (s *Server) Base() string { return s.base }

func (s *Server) handler(host string) http.Handler {
	mux := http.NewServeMux()
	for _, route := range Routes() {
		method, path, _ := strings.Cut(route.Pattern, " ")
		handle := route.Handle
		mux.HandleFunc(method+" /api/workspaces/"+Workspace+path, func(w http.ResponseWriter, r *http.Request) {
			handle(w, r, s.Root)
		})
	}
	mux.HandleFunc("GET /studio.json", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"id": Workspace, "path": s.Root, "name": filepath.Base(s.Root), "csrf": s.token})
	})
	files := http.FileServer(http.Dir(s.Dir))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/"+StudioPage, http.StatusFound)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only this loopback origin: no DNS rebinding, and a state change
		// carries the token only the studio page can read.
		if r.Host != host {
			WriteError(w, http.StatusForbidden, "wrong host")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Atlas-CSRF") != s.token {
			WriteError(w, http.StatusForbidden, "missing studio token")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

// RendererDir finds the desktop app's renderer build that holds the
// studio page: ATLAS_DESIGN_STUDIO_DIR, the installed app next to the
// executable, or a source checkout's app/renderer/dist.
func RendererDir() (string, error) {
	var dirs []string
	if d := os.Getenv("ATLAS_DESIGN_STUDIO_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	up := func(start string) {
		for dir := start; ; dir = filepath.Dir(dir) {
			dirs = append(dirs,
				filepath.Join(dir, "app", "resources", "renderer"),
				filepath.Join(dir, "app", "renderer", "dist"),
			)
			if filepath.Dir(dir) == dir {
				return
			}
		}
	}
	if exe, err := os.Executable(); err == nil {
		up(filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		up(cwd)
	}
	for _, d := range dirs {
		if info, err := os.Stat(filepath.Join(d, StudioPage)); err == nil && !info.IsDir() {
			return d, nil
		}
	}
	return "", errors.New("the design studio page is not built: run `npm run build` in the app directory, or set ATLAS_DESIGN_STUDIO_DIR to a renderer build holding " + StudioPage)
}

// String describes the server for logs.
func (s *Server) String() string { return fmt.Sprintf("design studio %s for %s", s.base, s.Root) }
