package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"podhod/server/internal/httpapi"
	"podhod/server/internal/service"
)

func TestNutritionRoutesEnforceIdentityScopeAndStrictJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, path, owner, body string
		scopes                          []string
		status                          int
	}{
		{name: "anonymous day", method: "GET", path: "/v1/nutrition/days/2026-10-31", status: 401},
		{name: "anonymous goals", method: "GET", path: "/v1/nutrition/goals", status: 401},
		{name: "anonymous recipes", method: "GET", path: "/v1/nutrition/recipes", status: 401},
		{name: "anonymous preferences", method: "GET", path: "/v1/nutrition/preferences", status: 401},
		{name: "schedule scope denied", method: "GET", path: "/v1/nutrition/goals",
			owner:  "owner",
			scopes: []string{"schedule:read"},
			status: 403},
		{name: "read cannot write", method: "PUT", path: "/v1/nutrition/goals",
			owner:  "owner",
			scopes: []string{"workout:read"},
			status: 403},
		{name: "invalid calendar route", method: "GET", path: "/v1/nutrition/days/2026-02-30",
			owner:  "owner",
			scopes: []string{"workout:read"},
			status: 400},
		{name: "owner body forbidden", method: "PUT", path: "/v1/nutrition/preferences",
			owner:  "owner",
			scopes: []string{"workout:write"},
			body:   `{"owner":"foreign"}`,
			status: 400},
		{name: "unknown nested field", method: "PUT", path: "/v1/nutrition/preferences",
			owner:  "owner",
			scopes: []string{"workout:write"},
			body:   `{"document":{"version":1,"hidden":"value"}}`,
			status: 400},
		{name: "two JSON objects", method: "PUT", path: "/v1/nutrition/preferences",
			owner:  "owner",
			scopes: []string{"workout:write"},
			body:   `{} {}`,
			status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verifier := func(context.Context, string, *http.Request) (*sdkauth.TokenInfo, error) {
				return &sdkauth.TokenInfo{UserID: tc.owner, Scopes: tc.scopes, Expiration: time.Now().Add(time.Hour)}, nil
			}
			handler := sdkauth.RequireBearerToken(verifier, nil)(httpapi.New(service.New(nil)))
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer fixture")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("HTTP %d, expected %d: %s", response.Code, tc.status, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" && tc.status == 400 {
				t.Fatal("private response can be cached")
			}
		})
	}
}

func TestNutritionCatalogIsPublicWithoutOpeningPrivateRoutes(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/nutrition/catalog", httpapi.PublicNutritionCatalog)
	mux.Handle("/v1/", httpapi.New(service.New(nil)))
	for _, tc := range []struct {
		path   string
		status int
	}{
		{path: "/v1/nutrition/catalog", status: 200},
		{path: "/v1/nutrition/recipes", status: 401},
		{path: "/v1/nutrition/days/2026-10-31", status: 401},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("HTTP %d, expected %d", response.Code, tc.status)
			}
			if tc.status == 200 && !json.Valid(response.Body.Bytes()) {
				t.Fatal("catalog is not JSON")
			}
		})
	}
}
