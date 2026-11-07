package ws

import (
	"encoding/json"
	"github.com/AlexxIT/go2rtc/internal/mediaauth"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedWebSocketLeaseAndOperationBoundary(t *testing.T) {
	var revoked atomic.Bool
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mediaauth.Rights{View: !revoked.Load()})
	}))
	defer authority.Close()
	u, _ := url.Parse(authority.URL)
	mediaauth.Port, _ = strconv.Atoi(u.Port())
	defer func() { mediaauth.Port = 0 }()
	initWS("")
	server := httptest.NewServer(http.HandlerFunc(apiWS))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "?src=owned-camera"
	_, res, err := websocket.DefaultDialer.Dial(endpoint, nil)
	require.Error(t, err)
	require.Equal(t, 403, res.StatusCode)
	res.Body.Close()
	socket, res, err := websocket.DefaultDialer.Dial(endpoint, http.Header{mediaauth.Header: []string{"11111111-1111-4111-8111-111111111111"}})
	require.NoError(t, err)
	defer socket.Close()
	require.Equal(t, "1", res.Header.Get(mediaauth.PolicyHeader))
	for _, operation := range []string{"hls", "webrtc/producer", "streams", "unknown"} {
		require.NoError(t, socket.WriteJSON(&Message{Type: operation, Value: "other-camera"}))
		var response Message
		require.NoError(t, socket.ReadJSON(&response))
		require.Equal(t, "error", response.Type)
	}
	revoked.Store(true)
	_ = socket.SetReadDeadline(time.Now().Add(7 * time.Second))
	_, _, err = socket.ReadMessage()
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "i/o timeout"), "revocation must close the actual upgraded transport")
}
