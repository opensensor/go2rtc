package ffmpeg

import (
	"testing"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/shell"
	"github.com/stretchr/testify/require"
)

func TestFFmpegSourceRejectsCommandFragments(t *testing.T) {
	for _, source := range []string{
		"virtual?video=movie%3D%2Fetc%2Fpasswd", "virtual?video=color&color=red%20-i%20file:///etc/passwd",
		"virtual?video=color&size=1x1,amovie=/etc/passwd", "virtual?video=testsrc&format=rgb24,movie=/etc/passwd",
		"virtual?video=testsrc&video=movie", "file:///etc/passwd", "/etc/passwd", "concat:http://camera/a|file:///etc/passwd", "device?video=0",
		"rtsp://camera/live#raw=-i", "rtsp://camera/live#input=-i", "rtsp://camera/live#video=-y", "rtsp://camera/live#audio=-y",
		"rtsp://camera/live#timeout=1 -i /etc/passwd", "rtsp://camera/live#bitrate=1 -y", "rtsp://camera/live#width=1,movie=/etc/passwd",
		"rtsp://camera/live#drawtext=textfile=/etc/passwd", "rtsp://camera/live#query=arbitrary", "rtsp://camera/live#hardware=-i",
	} {
		require.Error(t, validateSource(source), source)
	}
	for _, source := range []string{
		"virtual?video=testsrc&size=640x480&rate=5#video=h264", "virtual?video=color&color=blue&size=720#video=h264",
		"rtsp://camera/live#video=h264#audio=opus#input=rtsp/udp",
		"http://camera/video#video=h265#width=1280#height=720#bitrate=2M#framerate=30000/1001",
		"rtsp://camera/live#audio=auto", "rtsp://camera/live#video=copy#hardware=vaapi",
	} {
		require.NoError(t, validateSource(source), source)
	}
}

func TestFFmpegArgvPreservesInputBoundary(t *testing.T) {
	source := `rtsp://camera/" -i file:///tmp/injected -y "`
	args := parseArgs(source + "#video=h264")
	argv := args.Argv()
	inputs := 0
	for i, arg := range argv {
		if arg == "-i" {
			inputs++
			require.Equal(t, source, argv[i+1])
		}
		require.NotEqual(t, "-y", arg)
	}
	require.Equal(t, 1, inputs)
}

func TestFFmpegArgvMatchesSupportedTemplates(t *testing.T) {
	for _, source := range []string{
		"rtsp://camera/live#video=h264#audio=opus",
		"rtsp://camera/live#video=h265#width=1280#height=720",
		"http://camera/video#video=mjpeg#framerate=5",
		"rtsp://camera/live#audio=pcmu", "rtsp://camera/live#video=h264#video=h265#audio=aac#audio=opus",
		"rtsp://camera/live#video=h264#hardware=vaapi",
	} {
		args := parseArgs(source)
		require.Equal(t, shell.QuoteSplit(args.String()), args.Argv(), source)
	}
}

func TestFFmpegValidationAppliesBeforeRegistration(t *testing.T) {
	_, err := streams.New("unsafe", "ffmpeg:virtual?video=movie%3D%2Fetc%2Fpasswd#video=mjpeg")
	require.Error(t, err)
	require.Nil(t, streams.Get("unsafe"))
}

func TestFFmpegMissingStreamCannotBecomeLocalFile(t *testing.T) {
	require.NoError(t, streams.Validate("ffmpeg:passwd#video=h264"))
	args := parseArgsMode("passwd#video=h264", true).Argv()
	for i, arg := range args {
		if arg == "-i" {
			require.Contains(t, args[i+1], "rtsp://127.0.0.1:")
		}
	}
}

func TestFFmpegAcceptsLightNVRCodecTemplates(t *testing.T) {
	saved := defaults["h264"]
	t.Cleanup(func() { defaults["h264"] = saved })
	defaults["h264"] = "-codec:v libx264 -g:v 30 -preset:v superfast"
	require.NoError(t, streams.Validate("ffmpeg:virtual?video=color&size=64x64#video=h264"))
}
