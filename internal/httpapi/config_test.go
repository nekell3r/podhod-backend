package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"podhod/server/internal/httpapi"
)

func TestMobileConfigurationPreservesIOSDefaultAndSelectsAndroid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, query, clientID string
		status                int
	}{
		{name: "legacy iOS", clientID: "podhod-ios", status: 200},
		{name: "explicit iOS", query: "?platform=ios", clientID: "podhod-ios", status: 200},
		{name: "Android", query: "?platform=android", clientID: "podhod-android", status: 200},
		{name: "unknown platform", query: "?platform=web", status: 400},
		{name: "ambiguous platform", query: "?platform=android&platform=ios", status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := httpapi.MobileConfiguration("https://id.example.com/realms/podhod", []string{"schedule:read"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/config"+tc.query, nil))
			if response.Code != tc.status {
				t.Fatalf("HTTP %d, want %d", response.Code, tc.status)
			}
			if tc.status != http.StatusOK {
				return
			}
			var config struct {
				Issuer   string   `json:"issuer"`
				ClientID string   `json:"clientID"`
				Scopes   []string `json:"scopes"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &config); err != nil {
				t.Fatal(err)
			}
			if config.ClientID != tc.clientID || config.Issuer != "https://id.example.com/realms/podhod" {
				t.Fatalf("unexpected config: %+v", config)
			}
			if !slices.Equal(config.Scopes, []string{"openid", "offline_access", "schedule:read"}) {
				t.Fatalf("unexpected scopes: %v", config.Scopes)
			}
		})
	}
}
