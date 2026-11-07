package mediaauth

import (
	"encoding/json"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestLeaseBindingFailureAndRevocation(t *testing.T) {
	var status atomic.Int32
	status.Store(200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/media-lease" || r.URL.Query().Get("src") != "camera_sub" || r.Header.Get(Header) != "11111111-1111-4111-8111-111111111111" {
			w.WriteHeader(403)
			return
		}
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("Cookie"))
		w.WriteHeader(int(status.Load()))
		_ = json.NewEncoder(w).Encode(Rights{View: true})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	Port, _ = strconv.Atoi(u.Port())
	defer func() { Port = 0 }()
	request := httptest.NewRequest("GET", "http://local/api/ws?src=camera_sub", nil)
	_, err := Acquire(request)
	require.Error(t, err)
	request.Header.Set(Header, "11111111-1111-4111-8111-111111111111")
	lease, err := Acquire(request)
	require.NoError(t, err)
	media := []*core.Media{{Kind: core.KindVideo, Direction: core.DirectionSendonly}, {Kind: core.KindVideo, Direction: core.DirectionRecvonly}, {Kind: core.KindAudio, Direction: core.DirectionSendonly}, {Kind: core.KindAudio, Direction: core.DirectionRecvonly}}
	require.Equal(t, media[:1], lease.Filter(media))
	request.URL.RawQuery = "src=other"
	_, err = Acquire(request)
	require.Error(t, err)
	request.URL.RawQuery = "src=camera_sub&dst=other"
	_, err = Acquire(request)
	require.Error(t, err)
	closed := make(chan struct{})
	stop := lease.Watch(func() { close(closed) })
	defer stop()
	status.Store(503)
	select {
	case <-closed:
	case <-time.After(7 * time.Second):
		t.Fatal("authority failure did not close media")
	}
}
