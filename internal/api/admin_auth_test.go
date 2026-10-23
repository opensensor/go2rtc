package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminAuthAlwaysRequired(t *testing.T) {
	for _, endpoint := range []string{"config", "restart", "exit"} {
		for _, method := range []string{"GET", "POST", "PATCH", "OPTIONS"} {
			for _, remote := range []string{"127.0.0.1:1234", "[::1]:1234", "192.0.2.1:1234", "@"} {
				for _, credentials := range []struct {
					user, pass string
					want       int
				}{
					{"", "", 401}, {"admin", "wrong", 401}, {"admin", "secret", 204},
				} {
					handler := requireAdmin("admin", "secret", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
					req := httptest.NewRequest(method, "/go2rtc/api/"+endpoint, nil)
					req.RemoteAddr = remote
					if credentials.user != "" {
						req.SetBasicAuth(credentials.user, credentials.pass)
					}
					res := httptest.NewRecorder()
					handler(res, req)
					require.Equal(t, credentials.want, res.Code, "%s %s %s", endpoint, method, remote)
				}
			}
		}
	}
}

func TestAdminDisabledWithoutCredentials(t *testing.T) {
	for _, credentials := range [][2]string{{"", ""}, {"admin", ""}, {"", "secret"}} {
		handler := requireAdmin(credentials[0], credentials[1], func(http.ResponseWriter, *http.Request) { t.Fatal("admin handler reached") })
		req := httptest.NewRequest("PATCH", "/api/config", nil)
		req.SetBasicAuth(credentials[0], credentials[1])
		res := httptest.NewRecorder()
		handler(res, req)
		require.Equal(t, 403, res.Code)
	}
}
