// このパッケージはhashigoのhandlerを使用して、serverを起動。hashibackendにアクセス
//

package hashiserver

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runetale/runetale/hashi/hashigo"
	"github.com/runetale/runetale/hashi/hashilocal"
	"github.com/runetale/runetale/log"
)

type HandleSet[T any] map[Handle]T

type Handle struct {
	v *byte
}

func (s *HandleSet[T]) Add(e T) Handle {
	h := Handle{new(byte)}
	if *s == nil {
		*s = make(HandleSet[T])
	}
	(*s)[h] = e
	return h
}

type waiterSet HandleSet[context.CancelFunc]

type Server struct {
	hb atomic.Pointer[hashilocal.HashiBackend]

	mu sync.Mutex

	logger *log.Logger

	backendWaiter waiterSet
}

func New(logger *log.Logger) *Server {
	return &Server{
		logger: logger,
	}
}

func (s *Server) Run(ctx context.Context, ln net.Listener) error {
	defer func() {
		if hb := s.hb.Load(); hb != nil {
			hb.Shutdown()
		}
	}()

	runDone := make(chan struct{})
	defer close(runDone)

	go func() {
		select {
		case <-ctx.Done():
		case <-runDone:
		}
		ln.Close()
	}()

	hs := &http.Server{
		Handler:     http.HandlerFunc(s.serveHTTP),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
		IdleTimeout: 6 * time.Second,
	}

	if err := hs.Serve(ln); err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return err
	}
	return nil
}

func (s *Server) serveServerStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var res struct {
		Error string `json:"error,omitempty"`
	}

	lb := s.hb.Load()
	if lb == nil {
		res.Error = "backend not ready"
	}
	json.NewEncoder(w).Encode(res)
}

func (s *Server) awaitBackend(ctx context.Context) (_ *hashilocal.HashiBackend, ok bool) {
	lb := s.hb.Load()
	if lb != nil {
		return lb, true
	}

	ready, cleanup := s.backendWaiter.add(&s.mu, ctx)
	defer cleanup()

	lb = s.hb.Load()
	if lb != nil {
		return lb, true
	}

	<-ready
	lb = s.hb.Load()
	return lb, lb != nil
}

func (s *waiterSet) add(mu *sync.Mutex, ctx context.Context) (ready <-chan struct{}, cleanup func()) {
	ctx, cancel := context.WithCancel(ctx)
	hs := (*HandleSet[context.CancelFunc])(s) // change method set
	mu.Lock()
	h := hs.Add(cancel)
	mu.Unlock()
	return ctx.Done(), func() {
		mu.Lock()
		delete(*hs, h)
		mu.Unlock()
		cancel()
	}
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && r.URL.Path == "/server-status" {
		s.serveServerStatus(w, r)
		return
	}

	ctx := r.Context()
	hb, ok := s.awaitBackend(ctx)
	if !ok {
		http.Error(w, "no backend", http.StatusServiceUnavailable)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/localapi/") {
		lah := hashigo.NewHandler(hb)
		lah.ServeHTTP(w, r)
		return
	}

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	io.WriteString(w, "<html><title>Runetale</title><body><h1>Runetale</h1>hi, i'm Runetale daemon server.\n")
}

// 必ずbackendをセットする
func (s *Server) setBackend() *hashilocal.HashiBackend {
	lb := s.hb.Load()
	if lb == nil {
		panic("unexpected error, have to set hashibackend")
	}
	return lb
}
