package mcptransport

import (
	"context"
	"errors"
	"log/slog"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"podhod/server/internal/service"
)

type toolError struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	CurrentRevision *int64 `json:"current_revision,omitempty"`
}

type templateResult struct {
	Template *service.TemplateView `json:"template,omitempty"`
	Error    *toolError            `json:"error,omitempty"`
}

type weekResult struct {
	Week  *service.WeekView `json:"week,omitempty"`
	Error *toolError        `json:"error,omitempty"`
}

type activeResult struct {
	Active         *service.ActiveRecord `json:"active,omitempty"`
	DeliveryStatus string                `json:"delivery_status,omitempty"`
	Notice         string                `json:"notice,omitempty"`
	Error          *toolError            `json:"error,omitempty"`
}

const pendingSyncNotice = "Server state updated. " +
	"The iPhone receives this change on its next successful sync; " +
	"application on the phone is not acknowledged yet."

func serviceFailure(ctx context.Context, tool string, err error) (*sdkmcp.CallToolResult, *toolError) {
	detail := &toolError{
		Code:    "internal_error",
		Message: "The server could not complete this action. Retry later.",
	}
	var domainError *service.Error
	if errors.As(err, &domainError) {
		detail.Code = domainError.Code
		detail.Message = domainError.Message
		if domainError.Code == "revision_conflict" {
			revision := domainError.CurrentRevision
			detail.CurrentRevision = &revision
		}
	} else {
		slog.ErrorContext(
			ctx,
			"mcp operation failed",
			"tool",
			tool,
			"error",
			err,
		)
	}
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: detail.Message}},
	}, detail
}
