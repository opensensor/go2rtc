package webrtc

import (
	"bytes"
	"encoding/json"
	"github.com/AlexxIT/go2rtc/internal/api/ws"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/internal/mediaauth"
	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	rtc "github.com/AlexxIT/go2rtc/pkg/webrtc"
	"github.com/pion/rtp"
	pion "github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"
)

type ownedCamera struct {
	core.Connection
	talk atomic.Int64
}

func (c *ownedCamera) Start() error { return nil }
func (c *ownedCamera) AddTrack(m *core.Media, codec *core.Codec, recv *core.Receiver) error {
	sender := core.NewSender(m, codec)
	sender.Handler = func(*rtp.Packet) { c.talk.Add(1) }
	sender.HandleRTP(recv)
	c.Senders = append(c.Senders, sender)
	return nil
}

// Test actual loopback DTLS/SRTP packets in both directions, not SDP text or a
// requested audio= selector. The camera and browser are entirely test-owned.
func TestMediaAuthorityPackets(t *testing.T) {
	oldMux := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	defer func() { http.DefaultServeMux = oldMux }()
	ws.Init()
	ws.HandleFunc("webrtc/offer", asyncHandler)
	ws.HandleFunc("webrtc/candidate", candidateHandler)
	http.HandleFunc("/api/webrtc", syncHandler)
	signaling := httptest.NewServer(http.DefaultServeMux)
	defer signaling.Close()
	for _, protocol := range []string{"http", "ws"} {
		for _, mode := range []string{"video", "listen", "talk", "both"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				listen, talk := mode == "listen" || mode == "both", mode == "talk" || mode == "both"
				var revoked atomic.Bool
				authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "camera", r.URL.Query().Get("src"))
					require.Equal(t, "11111111-1111-4111-8111-111111111111", r.Header.Get(mediaauth.Header))
					_ = json.NewEncoder(w).Encode(mediaauth.Rights{View: !revoked.Load(), Listen: listen, Talk: talk})
				}))
				defer authority.Close()
				u, _ := url.Parse(authority.URL)
				mediaauth.Port, _ = strconv.Atoi(u.Port())
				defer func() { mediaauth.Port = 0 }()
				request := httptest.NewRequest("POST", "http://localhost/api/webrtc?src=camera&audio=all", nil)
				request.Header.Set(mediaauth.Header, "11111111-1111-4111-8111-111111111111")
				_, err := mediaauth.Acquire(request)
				require.NoError(t, err)
				api, err := rtc.NewAPI()
				require.NoError(t, err)
				PeerConnection = func(bool) (*pion.PeerConnection, error) { return api.NewPeerConnection(pion.Configuration{}) }
				browser, err := api.NewPeerConnection(pion.Configuration{})
				require.NoError(t, err)
				defer browser.Close()
				_, err = browser.AddTransceiverFromKind(pion.RTPCodecTypeVideo, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionRecvonly})
				require.NoError(t, err)
				microphone, err := pion.NewTrackLocalStaticRTP(pion.RTPCodecCapability{MimeType: pion.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "mic", "browser")
				require.NoError(t, err)
				_, err = browser.AddTransceiverFromTrack(microphone, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionSendrecv})
				require.NoError(t, err)
				var receivedVideo, receivedAudio atomic.Int64
				browser.OnTrack(func(track *pion.TrackRemote, _ *pion.RTPReceiver) {
					for {
						_, _, e := track.ReadRTP()
						if e != nil {
							return
						}
						if track.Kind() == pion.RTPCodecTypeVideo {
							receivedVideo.Add(1)
						} else {
							receivedAudio.Add(1)
						}
					}
				})
				offer, err := browser.CreateOffer(nil)
				require.NoError(t, err)
				gathering := pion.GatheringCompletePromise(browser)
				require.NoError(t, browser.SetLocalDescription(offer))
				<-gathering
				vc := &core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: 96, FmtpLine: "packetization-mode=1;profile-level-id=42e01f"}
				ac := &core.Codec{Name: core.CodecOpus, ClockRate: 48000, Channels: 2, PayloadType: 111}
				vm := &core.Media{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{vc}}
				am := &core.Media{Kind: core.KindAudio, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{ac}}
				tm := &core.Media{Kind: core.KindAudio, Direction: core.DirectionSendonly, Codecs: []*core.Codec{ac}}
				camera := &ownedCamera{Connection: core.Connection{Medias: []*core.Media{vm, am, tm}}}
				video, _ := camera.GetTrack(vm, vc)
				audio, _ := camera.GetTrack(am, ac)
				stream := streams.NewStream(nil)
				stream.AddProducer(camera)
				defer stream.Stop()
				// Register the exact owned camera selected by the signaling URL.
				registered, err := streams.New("camera")
				require.NoError(t, err)
				registered.AddProducer(camera)
				defer registered.Stop()
				if protocol == "http" {
					req, err := http.NewRequest("POST", signaling.URL+"/api/webrtc?src=camera&audio=all", bytes.NewBufferString(browser.LocalDescription().SDP))
					require.NoError(t, err)
					req.Header.Set("Content-Type", "application/sdp")
					req.Header.Set(mediaauth.Header, request.Header.Get(mediaauth.Header))
					response, err := http.DefaultClient.Do(req)
					require.NoError(t, err)
					answer, err := io.ReadAll(response.Body)
					response.Body.Close()
					require.NoError(t, err)
					require.Equal(t, 201, response.StatusCode, string(answer))
					require.Equal(t, "1", response.Header.Get(mediaauth.PolicyHeader))
					require.NoError(t, browser.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: string(answer)}))
				} else {
					headers := http.Header{mediaauth.Header: []string{request.Header.Get(mediaauth.Header)}}
					socket, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(signaling.URL, "http")+"/api/ws?src=camera&audio=all", headers)
					require.NoError(t, err)
					defer socket.Close()
					require.Equal(t, "1", response.Header.Get(mediaauth.PolicyHeader))
					require.NoError(t, socket.WriteJSON(&ws.Message{Type: "webrtc/offer", Value: browser.LocalDescription().SDP}))
					var answer ws.Message
					require.NoError(t, socket.ReadJSON(&answer))
					require.Equal(t, "webrtc/answer", answer.Type, answer.Value)
					require.NoError(t, browser.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.String()}))
					go func() {
						for {
							var msg ws.Message
							if socket.ReadJSON(&msg) != nil {
								return
							}
							if msg.Type == "webrtc/candidate" {
								_ = browser.AddICECandidate(pion.ICECandidateInit{Candidate: msg.String()})
							}
						}
					}()
				}

				stop := make(chan struct{})
				defer close(stop)
				go func() {
					ticker := time.NewTicker(20 * time.Millisecond)
					defer ticker.Stop()
					var seq uint16
					for {
						select {
						case <-stop:
							return
						case <-ticker.C:
							seq++
							video.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 1800, SSRC: 1, Marker: true}, Payload: []byte{0x65, 0x88, 0x84, 0x00}})
							audio.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 960, SSRC: 2}, Payload: []byte{0xf8, 0xff, 0xfe}})
							_ = microphone.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 960}, Payload: []byte{0xf8, 0xff, 0xfe}})
						}
					}
				}()
				require.Eventually(t, func() bool { return receivedVideo.Load() > 5 }, 5*time.Second, 20*time.Millisecond)
				time.Sleep(300 * time.Millisecond)
				if listen {
					require.Positive(t, receivedAudio.Load())
				} else {
					require.Zero(t, receivedAudio.Load())
				}
				if talk {
					require.Positive(t, camera.talk.Load())
				} else {
					require.Zero(t, camera.talk.Load())
				}
				t.Logf("actual packets video=%d listen=%d talk=%d", receivedVideo.Load(), receivedAudio.Load(), camera.talk.Load())
				revoked.Store(true)
				before := time.Now()
				time.Sleep(7 * time.Second)
				v, a, m := receivedVideo.Load(), receivedAudio.Load(), camera.talk.Load()
				time.Sleep(300 * time.Millisecond)
				require.Equal(t, v, receivedVideo.Load())
				require.Equal(t, a, receivedAudio.Load())
				require.Equal(t, m, camera.talk.Load())
				t.Logf("all packet delivery stopped within %s after view revocation", time.Since(before))
			})
		}
	}
}
