// Package testfixture owns a synthetic camera for protocol regression tests.
package testfixture

import (
	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/pion/rtp"
)

type Camera struct {
	core.Connection
	Video, Audio *core.Receiver
}

func New(name string) (*Camera, *streams.Stream, error) {
	vc := &core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: 96, FmtpLine: "packetization-mode=1;profile-level-id=64001f;sprop-parameter-sets=Z2QAH6wkhAFAFuwEQAAAAwBAAAAMI8YMkg==,aO4yyLA="}
	ac := &core.Codec{Name: core.CodecAAC, ClockRate: 48000, Channels: 2, PayloadType: core.PayloadTypeRAW, FmtpLine: "config=1190"}
	vm := &core.Media{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{vc}}
	am := &core.Media{Kind: core.KindAudio, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{ac}}
	c := &Camera{Connection: core.Connection{Medias: []*core.Media{vm, am}}}
	c.Video, _ = c.GetTrack(vm, vc)
	c.Audio, _ = c.GetTrack(am, ac)
	stream, err := streams.New(name)
	if err != nil {
		return nil, nil, err
	}
	stream.AddProducer(c)
	return c, stream, nil
}
func (c *Camera) Start() error { return nil }
func (c *Camera) Send(seq uint16) {
	c.Video.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 3000, SSRC: 1, Marker: true}, Payload: []byte{0x65, 0x88, 0x84, 0x00}})
	c.Audio.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 1024, SSRC: 2}, Payload: []byte{0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}})
}
