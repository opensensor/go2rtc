package streams

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecursion(t *testing.T) {
	resetStreamsForTest(t)
	registerProducerForTest(t, "rtsp")

	// create stream with some source
	stream1, err := New("from_yaml", "rtsp://does.not.matter")
	require.NoError(t, err)
	require.Len(t, streams, 1)

	// ask another unnamed stream that links go2rtc
	query, err := url.ParseQuery("src=rtsp://localhost:8554/from_yaml?video")
	require.NoError(t, err)
	stream2, err := GetOrPatch(query)
	require.NoError(t, err)

	// check stream is same
	require.Equal(t, stream1, stream2)
	// check stream urls is same
	require.Equal(t, stream1.producers[0].url, stream2.producers[0].url)
	require.Len(t, streams, 2)
}

func TestTempate(t *testing.T) {
	resetStreamsForTest(t)
	registerProducerForTest(t, "ffmpeg")
	registerProducerForTest(t, "rtsp")

	// config from yaml
	stream1, err := New("camera.from_hass", "ffmpeg:{input}#video=copy")
	require.NoError(t, err)
	// request from hass
	stream2, err := Patch("camera.from_hass", "rtsp://example.com")
	require.NoError(t, err)

	require.Equal(t, stream1, stream2)
	require.Equal(t, "ffmpeg:rtsp://example.com#video=copy", stream1.producers[0].url)
}
