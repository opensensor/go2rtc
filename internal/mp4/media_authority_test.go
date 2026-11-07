package mp4

import (
	"bytes"
	"encoding/json"
	"github.com/AlexxIT/go2rtc/internal/api/ws"
	"github.com/AlexxIT/go2rtc/internal/mediaauth"
	"github.com/AlexxIT/go2rtc/internal/mediaauth/testfixture"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestManagedMSEContainerTracks(t *testing.T) {
	for _, listen := range []bool{false, true} {
		t.Run(strconv.FormatBool(listen), func(t *testing.T) {
			authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(mediaauth.Rights{View: true, Listen: listen})
			}))
			defer authority.Close()
			u, _ := url.Parse(authority.URL)
			mediaauth.Port, _ = strconv.Atoi(u.Port())
			defer func() { mediaauth.Port = 0 }()
			camera, stream, err := testfixture.New("mse-fixture")
			require.NoError(t, err)
			defer stream.Stop()
			req := httptest.NewRequest("GET", "http://local/api/ws?src=mse-fixture&audio=all", nil)
			req.Header.Set(mediaauth.Header, "11111111-1111-4111-8111-111111111111")
			tr := &ws.Transport{Request: req}
			defer tr.Close()
			var mu sync.Mutex
			var output []byte
			tr.OnWrite(func(msg any) error {
				if data, ok := msg.([]byte); ok {
					mu.Lock()
					output = append(output, data...)
					mu.Unlock()
				}
				return nil
			})
			require.NoError(t, handlerWSMSE(tr, &ws.Message{Type: "mse", Value: "avc1.640029,mp4a.40.2"}))
			for seq := uint16(1); seq < 15; seq++ {
				camera.Send(seq)
				time.Sleep(20 * time.Millisecond)
			}
			mu.Lock()
			data := append([]byte(nil), output...)
			mu.Unlock()
			require.Contains(t, string(data), "vide")
			require.Contains(t, string(data), "moof")
			require.Contains(t, string(data), "mdat")
			require.Equal(t, listen, bytes.Contains(data, []byte("soun")), "actual fMP4 init audio track")
			t.Logf("fMP4 bytes=%d audio track=%t", len(data), bytes.Contains(data, []byte("soun")))
		})
	}
}
