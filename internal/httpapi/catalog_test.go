package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"podhod/server/internal/httpapi"
	"podhod/server/internal/service"
)

func TestCatalogRoutesValidateIdentityScopeAndPagination(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, owner string
		scopes            []string
		status            int
	}{
		{name: "identity required", path: "/v1/exercises", status: 401},
		{name: "scope required", path: "/v1/exercises", owner: "owner", scopes: []string{"workout:read"}, status: 403},
		{name: "catalog read", path: "/v1/exercises", owner: "owner", scopes: []string{"schedule:read"}, status: 200},
		{name: "limit bounded", path: "/v1/exercises?limit=101", owner: "owner", scopes: []string{"schedule:read"}, status: 400},
		{name: "zero explicit limit rejected", path: "/v1/exercises?limit=0", owner: "owner", scopes: []string{"schedule:read"}, status: 400},
		{name: "invalid limit", path: "/v1/exercises?limit=hello", owner: "owner", scopes: []string{"schedule:read"}, status: 400},
		{name: "negative offset", path: "/v1/exercises?offset=-1", owner: "owner", scopes: []string{"schedule:read"}, status: 400},
		{name: "unknown detail", path: "/v1/exercises/unknown", owner: "owner", scopes: []string{"schedule:read"}, status: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verifier := func(context.Context, string, *http.Request) (*sdkauth.TokenInfo, error) {
				return &sdkauth.TokenInfo{UserID: tc.owner, Scopes: tc.scopes, Expiration: time.Now().Add(time.Hour)}, nil
			}
			handler := sdkauth.RequireBearerToken(verifier, nil)(httpapi.New(service.New(nil)))
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			request.Header.Set("Authorization", "Bearer fixture")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("HTTP %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.status == 200 {
				var catalog service.ExerciseSearchResult
				if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil || catalog.Exercises == nil {
					t.Fatalf("invalid catalog response %s: %v", response.Body.String(), err)
				}
			}
		})
	}
}
