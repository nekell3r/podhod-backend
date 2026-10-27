package httpapi

import (
	"net/http"

	"podhod/server/internal/service"
)

func MobileConfiguration(issuer string, scopes []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		platforms := r.URL.Query()["platform"]
		if len(platforms) > 1 {
			respond(w, nil, &service.Error{Code: "invalid_platform", Message: "specify one mobile platform"})
			return
		}
		clientID := "podhod-ios"
		switch r.URL.Query().Get("platform") {
		case "", "ios":
		case "android":
			clientID = "podhod-android"
		default:
			respond(w, nil, &service.Error{Code: "invalid_platform", Message: "platform must be ios or android"})
			return
		}
		respond(w, map[string]any{
			"issuer":   issuer,
			"clientID": clientID,
			"scopes":   append([]string{"openid", "offline_access"}, scopes...),
		}, nil)
	}
}
