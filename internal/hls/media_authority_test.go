package hls

import (
	"bytes"
	"encoding/json"
	"github.com/AlexxIT/go2rtc/internal/mediaauth"
	"github.com/AlexxIT/go2rtc/internal/mediaauth/testfixture"
	"github.com/AlexxIT/go2rtc/pkg/mpegts"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestManagedHLSContainerTracks(t *testing.T) {
	for _, format := range []string{"ts", "mp4"} {
		for _, listen := range []bool{false, true} {
			t.Run(format+strconv.FormatBool(listen), func(t *testing.T) {
				authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewEncoder(w).Encode(mediaauth.Rights{View: true, Listen: listen})
				}))
				defer authority.Close()
				u, _ := url.Parse(authority.URL)
				mediaauth.Port, _ = strconv.Atoi(u.Port())
				defer func() { mediaauth.Port = 0 }()
				camera, stream, err := testfixture.New("hls-fixture")
				require.NoError(t, err)
				defer stream.Stop()
				target := "http://local/api/stream.m3u8?src=hls-fixture&audio=all"
				if format == "mp4" {
					target += "&mp4=all"
				} else {
					target = "http://local/api/stream.m3u8?src=hls-fixture"
				}
				req := httptest.NewRequest("GET", target, nil)
				req.Header.Set(mediaauth.Header, "11111111-1111-4111-8111-111111111111")
				res := httptest.NewRecorder()
				handlerStream(res, req)
				require.Equal(t, 200, res.Code)
				require.Equal(t, "1", res.Header().Get(mediaauth.PolicyHeader))
				var session *Session
				sessionsMu.RLock()
				for _, s := range sessions {
					session = s
				}
				sessionsMu.RUnlock()
				require.NotNil(t, session)
				defer func() { session.alive.Stop(); sessionsMu.Lock(); delete(sessions, session.id); sessionsMu.Unlock() }()
				for seq := uint16(1); seq < 15; seq++ {
					camera.Send(seq)
					time.Sleep(20 * time.Millisecond)
				}
				data := session.Segment()
				require.NotEmpty(t, data)
				if format == "mp4" {
					init := session.init
					require.Contains(t, string(init), "vide")
					require.Equal(t, listen, bytes.Contains(init, []byte("soun")))
					require.Contains(t, string(data), "mdat")
				} else {
					// Probe the real transport stream's PAT/PMT and encoded payloads.
					data = append(append([]byte(nil), session.init...), data...)
					producer, err := mpegts.Open(bytes.NewReader(data))
					require.NoError(t, err)
					hasVideo, hasAudio := false, false
					for _, m := range producer.GetMedias() {
						hasVideo = hasVideo || m.Kind == "video"
						hasAudio = hasAudio || m.Kind == "audio"
					}
					require.True(t, hasVideo)
					require.Equal(t, listen, hasAudio)
				}
				t.Logf("%s media bytes=%d audio=%t", format, len(data), listen)
			})
		}
	}
}
