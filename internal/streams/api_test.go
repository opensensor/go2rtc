package streams

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/stretchr/testify/require"
)

type testProducer struct {
	core.Connection
}

func (p *testProducer) Start() error {
	return nil
}

func resetStreamsForTest(t *testing.T) {
	t.Helper()

	streamsMu.Lock()
	prevStreams := streams
	streams = map[string]*Stream{}
	streamsMu.Unlock()

	t.Cleanup(func() {
		streamsMu.Lock()
		streams = prevStreams
		streamsMu.Unlock()
	})
}

func registerProducerForTest(t *testing.T, scheme string) {
	t.Helper()

	prevHandler, hadHandler := handlers[scheme]
	HandleFunc(scheme, func(url string) (core.Producer, error) {
		return &testProducer{}, nil
	})

	t.Cleanup(func() {
		if hadHandler {
			handlers[scheme] = prevHandler
		} else {
			delete(handlers, scheme)
		}
	})
}

func TestApiSchemes(t *testing.T) {
	SetReady()

	// Setup: Register some test handlers and redirects
	HandleFunc("rtsp", func(url string) (core.Producer, error) { return nil, nil })
	HandleFunc("rtmp", func(url string) (core.Producer, error) { return nil, nil })
	RedirectFunc("http", func(url string) (string, error) { return "", nil })

	t.Run("GET request returns schemes", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/schemes", nil)
		w := httptest.NewRecorder()

		apiSchemes(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var schemes []string
		err := json.Unmarshal(w.Body.Bytes(), &schemes)
		require.NoError(t, err)
		require.NotEmpty(t, schemes)

		// Check that our test schemes are in the response
		require.Contains(t, schemes, "rtsp")
		require.Contains(t, schemes, "rtmp")
		require.Contains(t, schemes, "http")
	})
}

func TestApiSchemesNoDuplicates(t *testing.T) {
	SetReady()

	// Setup: Register a scheme in both handlers and redirects
	HandleFunc("duplicate", func(url string) (core.Producer, error) { return nil, nil })
	RedirectFunc("duplicate", func(url string) (string, error) { return "", nil })

	req := httptest.NewRequest("GET", "/api/schemes", nil)
	w := httptest.NewRecorder()

	apiSchemes(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var schemes []string
	err := json.Unmarshal(w.Body.Bytes(), &schemes)
	require.NoError(t, err)

	// Count occurrences of "duplicate"
	count := 0
	for _, scheme := range schemes {
		if scheme == "duplicate" {
			count++
		}
	}

	// Should only appear once
	require.Equal(t, 1, count, "scheme 'duplicate' should appear exactly once")
}

func TestApiSchemesWaitsForReady(t *testing.T) {
	oldReady := ready
	oldReadyOnce := readyOnce
	ready = make(chan struct{})
	readyOnce = sync.Once{}
	t.Cleanup(func() {
		ready = oldReady
		readyOnce = oldReadyOnce
	})

	HandleFunc("waittest", func(url string) (core.Producer, error) { return nil, nil })

	req := httptest.NewRequest("GET", "/api/schemes", nil)
	w := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		apiSchemes(w, req)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("apiSchemes returned before streams became ready")
	case <-time.After(50 * time.Millisecond):
	}

	SetReady()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("apiSchemes did not return after streams became ready")
	}

	require.Equal(t, http.StatusOK, w.Code)

	var schemes []string
	err := json.Unmarshal(w.Body.Bytes(), &schemes)
	require.NoError(t, err)
	require.Contains(t, schemes, "waittest")
}

func TestApiReload(t *testing.T) {
	resetStreamsForTest(t)

	streamsMu.Lock()
	streams["front_door"] = NewStream("rtsp://example.local/live")
	streamsMu.Unlock()

	req := httptest.NewRequest("POST", "/api/reload?src=front_door", nil)
	w := httptest.NewRecorder()

	apiReload(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.NotNil(t, Get("front_door"), "reload must keep stream registered")

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "front_door", body["src"])
	require.Equal(t, "reloaded", body["status"])
	require.Equal(t, []string{"rtsp://example.local/live"}, Get("front_door").Sources())
}

func TestApiReloadErrors(t *testing.T) {
	resetStreamsForTest(t)

	tests := []struct {
		name string
		req  string
		code int
	}{
		{name: "method not allowed", req: "GET /api/reload?src=front_door", code: http.StatusMethodNotAllowed},
		{name: "missing source", req: "POST /api/reload", code: http.StatusBadRequest},
		{name: "not found", req: "POST /api/reload?src=missing", code: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, target, ok := strings.Cut(tt.req, " ")
			require.True(t, ok)
			req := httptest.NewRequest(method, target, nil)
			w := httptest.NewRecorder()

			apiReload(w, req)

			require.Equal(t, tt.code, w.Code)
		})
	}
}
