package mcptransport

import (
	"fmt"
	"slices"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ScopeScheduleRead  = "schedule:read"
	ScopeScheduleWrite = "schedule:write"
	ScopeWorkoutRead   = "workout:read"
	ScopeWorkoutWrite  = "workout:write"
)

type securityScheme struct {
	Type   string   `json:"type"`
	Scopes []string `json:"scopes"`
}

func oauthMetadata(scope string) sdkmcp.Meta {
	scopes := []string{}
	if scope != "" {
		scopes = append(scopes, scope)
	}
	return sdkmcp.Meta{
		"securitySchemes": []securityScheme{{Type: "oauth2", Scopes: scopes}},
	}
}

func authorizeTool(
	req *sdkmcp.CallToolRequest,
	scope string,
	resourceMetadataURL string,
) (string, *sdkmcp.CallToolResult) {
	missingIdentity := req.Extra == nil || req.Extra.TokenInfo == nil
	if missingIdentity {
		return "", authorizationFailure(resourceMetadataURL, scope, "invalid_token")
	}

	identity := req.Extra.TokenInfo
	if strings.TrimSpace(identity.UserID) == "" {
		return "", authorizationFailure(resourceMetadataURL, scope, "invalid_token")
	}
	if scope != "" && !slices.Contains(identity.Scopes, scope) {
		return "", authorizationFailure(resourceMetadataURL, scope, "insufficient_scope")
	}
	return identity.UserID, nil
}

func authorizationFailure(resourceMetadataURL, scope, code string) *sdkmcp.CallToolResult {
	message := "Connect your Podhod account before using this tool."
	if code == "insufficient_scope" {
		message = "This connection does not have permission for this action. Reconnect with the required permission."
	}
	challenge := fmt.Sprintf(
		"Bearer resource_metadata=%q, error=%q, error_description=%q",
		resourceMetadataURL,
		code,
		message,
	)
	if scope != "" {
		challenge += fmt.Sprintf(", scope=%q", scope)
	}
	return &sdkmcp.CallToolResult{
		IsError: true,
		Meta: sdkmcp.Meta{
			"mcp/www_authenticate": []string{challenge},
		},
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: message}},
	}
}
