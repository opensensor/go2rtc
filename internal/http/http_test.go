package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// A rejected producer must close the body even when the server keeps sending.
// Waiting for request cancellation observes client-side closure, without
// depending on process FD counts or a server that closes the socket itself.
func TestRejectedProducerClosesResponseBody(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			closed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "1048576")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("unread error body"))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(closed)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel() // Also releases the handler if the regression fails.
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			producer, err := do(req)
			if producer != nil || err == nil || err.Error() != strconv.Itoa(status)+" "+http.StatusText(status) {
				t.Fatalf("unexpected rejected producer: %v, %v", producer, err)
			}
			select {
			case <-closed:
			case <-time.After(3 * time.Second):
				t.Fatal("rejected response body kept its connection open")
			}
		})
	}
}
