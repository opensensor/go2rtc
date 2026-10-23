package ffmpeg

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestArgvKeepsFilterAsOneArgument(t *testing.T) {
	filter := `drawtext=text=" -i /tmp/injected -y "`
	args := &Args{Bin: "/path with spaces/ffmpeg", Input: "-i {input}", InputValue: `http://camera/" -y "`, Filters: []string{filter}, Output: "-f mjpeg -"}
	require.Equal(t, []string{"/path with spaces/ffmpeg", "-i", `http://camera/" -y "`, "-vf", filter, "-f", "mjpeg", "-"}, args.Argv())
}
