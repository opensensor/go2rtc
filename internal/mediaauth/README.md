# Managed LightNVR media authority

Standalone go2rtc remains unchanged when `api.media_authority_port` is absent.
LightNVR pins this port in its final managed config. Its proxy creates an opaque,
122-bit random lease and sends `X-LightNVR-Media-Lease`; browser credentials are
never forwarded. `X-LightNVR-Media-TLS: 1` selects the owning local TLS listener.
The callback destination is always 127.0.0.1 at the configured port, redirects
and environment proxies are disabled, and the request deadline is two seconds.
TLS verification is intentionally limited to the local self-signed listener.
The callback contains no passwords, cookies, API keys, or camera credentials.

`GET /api/internal/media-lease?src=<canonical source>` returns JSON booleans
`view`, `listen`, and `talk`. LightNVR validates the opaque lease, actual loopback
peer, original principal, immutable camera identity, current account/session/key/
token validity, policy and token camera/action intersection. A failed callback
or any change in rights terminates the peer/session/transport. Renewal occurs
every five seconds: the established-media revocation bound is seven seconds,
including callback timeout, provided the process scheduler continues to run.

Successful managed WebSocket, HTTP WebRTC and HLS manifest responses carry
`X-LightNVR-Media-Policy: 1`. LightNVR rejects successful responses without this
acknowledgment, including older independently managed go2rtc installations.
Independent deployments must configure the authority port and use a compatible
fork. There is no fallback that silently bypasses audio policy.

Media selection happens after client codec/SDP parsing and before track binding:
video is outbound-only; camera audio requires `listen`; microphone backchannel
requires `talk`. Client `audio` selectors and SDP directions cannot expand the
lease. Managed WebSocket messages are restricted to WebRTC offer/candidates,
MSE and MP4; WS publishing/HLS/other module commands are rejected. HTTP HLS
remains supported for both MPEG-TS and fMP4. WebSocket closure also closes its
WebRTC peers; HTTP peers renew independently of signaling connection lifetime.

Run owned, camera-free packet/container and revocation tests with:

```
go test -race ./internal/mediaauth ./internal/api/ws ./internal/webrtc ./internal/mp4 ./internal/hls ./pkg/core ./pkg/webrtc -count=1 -timeout=180s
```

The WebRTC tests exchange real SDP over HTTP and WebSocket, connect local Pion
DTLS/SRTP peers, transmit actual video/audio RTP and a microphone backchannel,
and measure cessation after revocation for all four listen/talk combinations.
HLS tests inspect real TS demuxer output and fMP4 track metadata/payloads; MSE
tests inspect actual muxed fMP4 output. No physical camera or user instance is
contacted.
