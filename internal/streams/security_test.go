package streams

import (
	"testing"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/stretchr/testify/require"
)

func TestProducerRejectsUnsafeRedirects(t *testing.T) {
	oldHandlers, oldRedirects := handlers, redirects
	t.Cleanup(func() { handlers, redirects = oldHandlers, oldRedirects })
	called := false
	handlers = map[string]Handler{"exec": func(string) (core.Producer, error) { called = true; return nil, nil }}
	for _, target := range []string{"exec:sh", "echo:unsafe", "expr:unsafe", `ffmpeg:rtsp://camera/live#raw=-i`, `rtsp://camera/"quoted"`} {
		redirects = map[string]Redirect{"test": func(string) (string, error) { return target, nil }}
		_, err := GetProducer("test:source")
		require.Error(t, err, target)
	}
	require.False(t, called)
}

func TestProducerSafeRedirectAndCycle(t *testing.T) {
	oldHandlers, oldRedirects := handlers, redirects
	t.Cleanup(func() { handlers, redirects = oldHandlers, oldRedirects })
	called := false
	handlers = map[string]Handler{"safe": func(string) (core.Producer, error) { called = true; return nil, nil }}
	redirects = map[string]Redirect{"test": func(string) (string, error) { return "safe:camera", nil }}
	_, err := GetProducer("test:source")
	require.NoError(t, err)
	require.True(t, called)
	redirects["test"] = func(string) (string, error) { return "test:cycle", nil }
	_, err = GetProducer("test:source")
	require.ErrorContains(t, err, "too many redirects")
}

func TestConfigStreamRejectsUnsafeSources(t *testing.T) {
	for _, source := range []any{
		"exec:sh", []string{"rtsp://camera/live", "exec:sh"},
		[]any{"rtsp://camera/live", "echo:unsafe"},
		map[string]any{"url": []any{"expr:unsafe"}},
		`ffmpeg:rtsp://camera/live#raw=-i`, []any{42}, 42,
	} {
		stream, err := configStream(source)
		require.Error(t, err, "%v", source)
		require.Nil(t, stream)
	}
	for _, source := range []any{"rtsp://camera/live", []any{"rtsp://camera/live", nil}, map[string]any{"url": "rtsp://camera/live"}, nil} {
		stream, err := configStream(source)
		require.NoError(t, err)
		require.NotNil(t, stream)
	}
}
