package streams

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRejectsFFmpegArgumentInjection(t *testing.T) {
	err := Validate(`ffmpeg:rtsp://127.0.0.1:8554/testsrc#raw="-i""file:///tmp/POC_RCE_INJECT_MARKER"`)
	require.ErrorContains(t, err, "ffmpeg raw params")
}

func TestValidateRejectsShellQuotes(t *testing.T) {
	err := Validate(`rtsp://example.com/"stream"`)
	require.ErrorContains(t, err, "shell quotes")
}

func TestValidateAllowsCommonFFmpegParams(t *testing.T) {
	err := Validate(`ffmpeg:rtsp://example.com/stream#video=h264#input=rtsp/udp`)
	require.NoError(t, err)
}

func TestConfigRejectsRawFFmpeg(t *testing.T) {
	_, err := configStream(`ffmpeg:rtsp://127.0.0.1:8554/testsrc#raw="-i""file:///tmp/config_input"`)
	require.Error(t, err)
}
