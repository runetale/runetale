// このパッケージはrunetaledで動いている、hashilocalにアクセスするためのhashiserverのapi handlerです

package hashigo

import (
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/runetale/runetale/hashi/hashilocal"
)

type hashigoAPIHandler func(*Handler, http.ResponseWriter, *http.Request)

var hashigoApiHandler = map[string]hashigoAPIHandler{
	"ping":   (*Handler).ping,
	"logout": (*Handler).logout,
	"dial":   (*Handler).dial,
}

type Handler struct {
	RequiredPassword string

	b *hashilocal.HashiBackend
}

func NewHandler(b *hashilocal.HashiBackend) *Handler {
	return &Handler{b: b}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.b == nil {
		http.Error(w, "server has no local backend", http.StatusInternalServerError)
		return
	}
	if r.Referer() != "" || r.Header.Get("Origin") != "" || !h.validHost(r.Host) {
		http.Error(w, "invalid localapi request", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Security-Policy", `default-src 'none'; frame-ancestors 'none'; script-src 'none'; script-src-elem 'none'; script-src-attr 'none'`)
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if h.RequiredPassword != "" {
		_, pass, ok := r.BasicAuth()
		if !ok {
			http.Error(w, "auth required", http.StatusUnauthorized)
			return
		}
		if pass != h.RequiredPassword {
			http.Error(w, "bad password", http.StatusForbidden)
			return
		}
	}
	if fn, ok := handlerForPath(r.URL.Path); ok {
		fn(h, w, r)
	} else {
		http.NotFound(w, r)
	}
}

func (h *Handler) validHost(hostname string) bool {
	if h.RequiredPassword == "" {
		return false
	}
	host, _, err := net.SplitHostPort(hostname)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback()
}

func (*Handler) serveLocalAPIRoot(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "runetaled\n")
}

func handlerForPath(urlPath string) (h hashigoAPIHandler, ok bool) {
	if urlPath == "/" {
		return (*Handler).serveLocalAPIRoot, true
	}

	suff, ok := strings.CutPrefix(urlPath, "/localapi/v0/")
	if !ok {
		return nil, false
	}

	if fn, ok := hashigoApiHandler[suff]; ok {
		return fn, true
	}

	return nil, false
}

func (h *Handler) ping(w http.ResponseWriter, r *http.Request) {
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
}

// netstack gvisor<=>wireguardを通して、tcp or udpの接続を確立する。
func (h *Handler) dial(w http.ResponseWriter, r *http.Request) {
}
