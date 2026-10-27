package mcptransport

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"podhod/server/internal/service"
)

type exerciseSearchInput struct {
	Query     string `json:"query,omitempty" jsonschema:"Case-insensitive Russian name or English alias fragment."`
	Muscle    string `json:"muscle,omitempty" jsonschema:"Muscle ID from the catalog; primary or secondary."`
	Equipment string `json:"equipment,omitempty" jsonschema:"Equipment ID from the catalog."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Page size from 1 to 100; default 20."`
	Offset    int    `json:"offset,omitempty" jsonschema:"Nonnegative result offset; default 0."`
}

type exerciseIDInput struct {
	ExerciseID string `json:"exercise_id" jsonschema:"Catalog UUID from search_exercises, not planned exercise occurrence ID."`
}

type catalogSearchResult struct {
	Result *service.ExerciseSearchResult `json:"result,omitempty"`
	Error  *toolError                    `json:"error,omitempty"`
}

type catalogDetailResult struct {
	Result *service.ExerciseDetail `json:"result,omitempty"`
	Error  *toolError              `json:"error,omitempty"`
}

func (h *handler) searchExercises(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input exerciseSearchInput,
) (*sdkmcp.CallToolResult, catalogSearchResult, error) {
	_, denied := authorizeTool(req, ScopeScheduleRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, catalogSearchResult{}, nil
	}
	result, err := h.service.SearchExercises(service.ExerciseSearch{
		Query: input.Query, Muscle: input.Muscle, Equipment: input.Equipment,
		Limit: input.Limit, Offset: input.Offset,
	})
	if err != nil {
		failure, detail := serviceFailure(ctx, "search_exercises", err)
		return failure, catalogSearchResult{Error: detail}, nil
	}
	return nil, catalogSearchResult{Result: &result}, nil
}

func (h *handler) getExercise(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input exerciseIDInput,
) (*sdkmcp.CallToolResult, catalogDetailResult, error) {
	_, denied := authorizeTool(req, ScopeScheduleRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, catalogDetailResult{}, nil
	}
	result, err := h.service.GetExercise(input.ExerciseID)
	if err != nil {
		failure, detail := serviceFailure(ctx, "get_exercise", err)
		return failure, catalogDetailResult{Error: detail}, nil
	}
	return nil, catalogDetailResult{Result: &result}, nil
}
