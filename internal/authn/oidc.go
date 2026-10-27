package authn

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

func NewVerifier(ctx context.Context, issuer, audience string) (sdkauth.TokenVerifier, error) {
	if !strings.HasPrefix(issuer, "https://") || audience == "" {
		return nil, fmt.Errorf("https issuer and token audience are required")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), issuer)
	if err != nil {
		return nil, fmt.Errorf("discover identity provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{oidc.RS256}})
	return tokenVerifier(verifier), nil
}

func tokenVerifier(verifier *oidc.IDTokenVerifier) sdkauth.TokenVerifier {
	return func(ctx context.Context, raw string, request *http.Request) (*sdkauth.TokenInfo, error) {
		token, err := verifier.Verify(ctx, raw)
		if err != nil {
			return nil, fmt.Errorf("%w: verification failed", sdkauth.ErrInvalidToken)
		}
		var claims struct {
			Scope string `json:"scope"`
			Type  string `json:"typ"`
		}
		if err := token.Claims(&claims); err != nil {
			return nil, fmt.Errorf("%w: invalid claims", sdkauth.ErrInvalidToken)
		}
		if token.Subject == "" || claims.Type != "Bearer" {
			return nil, fmt.Errorf("%w: access token required", sdkauth.ErrInvalidToken)
		}
		return &sdkauth.TokenInfo{UserID: token.Subject, Scopes: strings.Fields(claims.Scope), Expiration: token.Expiry}, nil
	}
}
