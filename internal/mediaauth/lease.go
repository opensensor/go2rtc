// Package mediaauth enforces LightNVR's private, renewable media authority.
// Standalone go2rtc deployments remain unchanged unless explicitly configured.
package mediaauth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
)

const Header = "X-LightNVR-Media-Lease"
const PolicyHeader = "X-LightNVR-Media-Policy"
const Interval = 5 * time.Second

var Port int
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var client = &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, MaxIdleConnsPerHost: 16}}

// TLS is solely the loopback callback to the owning LightNVR listener. The
// callback carries an opaque random lease, never camera or user credentials.
type Rights struct {
	View   bool `json:"view"`
	Listen bool `json:"listen"`
	Talk   bool `json:"talk"`
}
type Lease struct {
	token, source, scheme string
	port                  int
	rights                Rights
}

func Enabled() bool { return Port > 0 }
func Acquire(r *http.Request) (*Lease, error) {
	if !Enabled() {
		return nil, nil
	}
	token := r.Header.Get(Header)
	if !tokenPattern.MatchString(token) || r.URL.Query().Get("dst") != "" || r.URL.Query().Get("src") == "" {
		return nil, errors.New("media authority required")
	}
	l := &Lease{token: token, source: r.URL.Query().Get("src"), scheme: "http", port: Port}
	if r.Header.Get("X-LightNVR-Media-TLS") == "1" {
		l.scheme = "https"
	}
	rights, err := l.check()
	if err != nil {
		return nil, err
	}
	l.rights = rights
	return l, nil
}
func (l *Lease) check() (Rights, error) {
	endpoint := fmt.Sprintf("%s://127.0.0.1:%d/api/internal/media-lease?src=%s", l.scheme, l.port, url.QueryEscape(l.source))
	r, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return Rights{}, err
	}
	r.Header.Set(Header, l.token)
	res, err := client.Do(r)
	if err != nil {
		return Rights{}, errors.New("media authority unavailable")
	}
	defer res.Body.Close()
	var rights Rights
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1024)).Decode(&rights) != nil || !rights.View {
		return Rights{}, errors.New("media authority denied")
	}
	return rights, nil
}
func (l *Lease) Filter(medias []*core.Media) []*core.Media {
	if l == nil {
		return medias
	}
	out := make([]*core.Media, 0, len(medias))
	for _, m := range medias {
		switch m.Kind {
		case core.KindVideo:
			if m.Direction == core.DirectionSendonly && l.rights.View {
				out = append(out, m)
			}
		case core.KindAudio:
			if m.Direction == core.DirectionSendonly && l.rights.Listen || m.Direction == core.DirectionRecvonly && l.rights.Talk {
				out = append(out, m)
			}
		}
	}
	return out
}

// Watch owns an independent lifetime: signaling disconnection cannot leave a
// WebRTC peer authorized forever. Any authority loss closes the entire peer;
// reconnect renegotiates under the new rights. stop is safe to call repeatedly.
func (l *Lease) Watch(closePeer func()) func() {
	if l == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	go func() {
		t := time.NewTicker(Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r, e := l.check()
				if e != nil || l.rights != r {
					closePeer()
					cancel()
					return
				}
			}
		}
	}()
	return func() { once.Do(cancel) }
}
