package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"podhod/server/internal/httpapi"
	"podhod/server/internal/service"
)

func TestCycleRoutesRequireScheduleScopeAndStrictJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, owner, body string
		scopes                    []string
		status                    int
	}{
		{name: "anonymous read", method: "GET", status: 401},
		{name: "anonymous write", method: "PUT", status: 401},
		{name: "workout read denied", method: "GET", owner: "owner", scopes: []string{"workout:read"}, status: 403},
		{name: "read cannot write", method: "PUT", owner: "owner", scopes: []string{"schedule:read"}, status: 403},
		{name: "owner forbidden", method: "PUT", owner: "owner", scopes: []string{"schedule:write"},
			body: `{"owner":"foreign"}`, status: 400},
		{name: "unknown document field", method: "PUT", owner: "owner", scopes: []string{"schedule:write"},
			body: `{"document":{"version":1,"hidden":true}}`, status: 400},
		{name: "unknown nested field", method: "PUT", owner: "owner", scopes: []string{"schedule:write"},
			body: `{"document":{"version":1,"plan":{"weeks":[{"days":[{"context":{"targetRIR":0}}]}]}}}`, status: 400},
		{name: "trailing JSON", method: "PUT", owner: "owner", scopes: []string{"schedule:write"}, body: `{} {}`, status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verify := func(context.Context, string, *http.Request) (*sdkauth.TokenInfo, error) {
				return &sdkauth.TokenInfo{UserID: tc.owner, Scopes: tc.scopes, Expiration: time.Now().Add(time.Hour)}, nil
			}
			handler := sdkauth.RequireBearerToken(verify, nil)(httpapi.New(service.New(nil)))
			request := httptest.NewRequest(tc.method, "/v1/cycle", strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer fixture")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("HTTP %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.status == 400 && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response can be cached")
			}
		})
	}
}
