package authn

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func TestAccessTokenValidation(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verify := tokenVerifier(oidc.NewVerifier("https://identity.example/realms/podhod",
		&oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}},
		&oidc.Config{ClientID: "podhod-api", SupportedSigningAlgs: []string{oidc.RS256}}))
	cases := []struct {
		name     string
		change   func(map[string]any)
		wrongKey bool
		valid    bool
	}{
		{name: "valid access token", valid: true},
		{name: "wrong issuer", change: func(c map[string]any) { c["iss"] = "https://attacker.example" }},
		{name: "wrong audience", change: func(c map[string]any) { c["aud"] = "another-api" }},
		{name: "expired", change: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{name: "missing subject", change: func(c map[string]any) { delete(c, "sub") }},
		{name: "ID token is not access token", change: func(c map[string]any) { c["typ"] = "ID" }},
		{name: "wrong signature", wrongKey: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"iss": "https://identity.example/realms/podhod", "aud": "podhod-api", "sub": "owner",
				"exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "typ": "Bearer", "scope": "schedule:read workout:read"}
			if tc.change != nil {
				tc.change(claims)
			}
			signingKey := key
			if tc.wrongKey {
				signingKey = other
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signingKey}, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(claims)
			if err != nil {
				t.Fatal(err)
			}
			signed, err := signer.Sign(data)
			if err != nil {
				t.Fatal(err)
			}
			compact, err := signed.CompactSerialize()
			if err != nil {
				t.Fatal(err)
			}
			token, err := verify(t.Context(), compact, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.valid && (token.UserID != "owner" || len(token.Scopes) != 2) {
				t.Fatalf("unexpected identity: %+v", token)
			}
		})
	}
}
