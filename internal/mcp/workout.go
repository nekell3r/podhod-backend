package mcptransport

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"podhod/server/internal/service"
)

type activeRestInput struct {
	SessionID                string `json:"session_id" jsonschema:"Session ID from get_active_workout."`
	ExerciseID               string `json:"exercise_id" jsonschema:"Session exercise ID, not the exercise catalog ID."`
	RestSeconds              int    `json:"between_sets_seconds" jsonschema:"Rest between sets in seconds, from 0 to 600."`
	RestAfterExerciseSeconds int    `json:"after_exercise_seconds" jsonschema:"Rest after exercise (0–600 seconds)."`
	ExpectedRevision         int64  `json:"expected_revision" jsonschema:"Revision from get_active_workout."`
	RequestID                string `json:"request_id" jsonschema:"UUID; reuse only for an identical retry."`
}

type skipInput struct {
	SessionID        string `json:"session_id" jsonschema:"Session ID from get_active_workout."`
	ExerciseID       string `json:"exercise_id" jsonschema:"Session exercise ID, not the exercise catalog ID."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Revision from get_active_workout."`
	RequestID        string `json:"request_id" jsonschema:"UUID; reuse only for an identical retry."`
}

type historyInput struct {
	Limit *int `json:"limit,omitempty" jsonschema:"Number of recent completed workouts, from 1 to 100. Defaults to 20."`
}

type historyResult struct {
	Sessions []service.WorkoutSession `json:"sessions"`
	Error    *toolError               `json:"error,omitempty"`
}

func (h *handler) getActive(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	_ emptyInput,
) (*sdkmcp.CallToolResult, activeResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, activeResult{}, nil
	}
	active, err := h.service.GetActive(ctx, owner)
	if err != nil {
		result, detail := serviceFailure(ctx, "get_active_workout", err)
		return result, activeResult{Error: detail}, nil
	}
	return nil, activeResult{Active: &active}, nil
}

func (h *handler) setActiveRest(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input activeRestInput,
) (*sdkmcp.CallToolResult, activeResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, activeResult{}, nil
	}
	active, err := h.service.SetActiveRest(ctx, owner, service.ActiveRestRequest{
		ExpectedRevision:         input.ExpectedRevision,
		RequestID:                input.RequestID,
		SessionID:                input.SessionID,
		ExerciseID:               input.ExerciseID,
		RestSeconds:              input.RestSeconds,
		RestAfterExerciseSeconds: input.RestAfterExerciseSeconds,
	})
	if err != nil {
		result, detail := serviceFailure(ctx, "update_active_exercise_rest", err)
		return result, activeResult{Error: detail}, nil
	}
	return nil, activeResult{
		Active:         &active,
		DeliveryStatus: "pending_sync",
		Notice:         pendingSyncNotice,
	}, nil
}

func (h *handler) skipExercise(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input skipInput,
) (*sdkmcp.CallToolResult, activeResult, error) {
	return h.setExerciseSkipped(
		ctx,
		req,
		input,
		true,
	)
}

func (h *handler) unskipExercise(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input skipInput,
) (*sdkmcp.CallToolResult, activeResult, error) {
	return h.setExerciseSkipped(
		ctx,
		req,
		input,
		false,
	)
}

func (h *handler) setExerciseSkipped(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input skipInput,
	skipped bool,
) (*sdkmcp.CallToolResult, activeResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, activeResult{}, nil
	}
	active, err := h.service.SkipExercise(ctx, owner, service.SkipExerciseRequest{
		ExpectedRevision: input.ExpectedRevision,
		RequestID:        input.RequestID,
		SessionID:        input.SessionID,
		ExerciseID:       input.ExerciseID,
		IsSkipped:        skipped,
	})
	if err != nil {
		name := "skip_active_exercise"
		if !skipped {
			name = "unskip_active_exercise"
		}
		result, detail := serviceFailure(ctx, name, err)
		return result, activeResult{Error: detail}, nil
	}
	return nil, activeResult{
		Active:         &active,
		DeliveryStatus: "pending_sync",
		Notice:         pendingSyncNotice,
	}, nil
}

func (h *handler) history(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input historyInput,
) (*sdkmcp.CallToolResult, historyResult, error) {
	result := historyResult{Sessions: []service.WorkoutSession{}}
	owner, denied := authorizeTool(req, ScopeWorkoutRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, result, nil
	}
	limit := 20
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > 100 {
		failure, detail := serviceFailure(ctx, "get_workout_history", &service.Error{
			Code:    "invalid_request",
			Message: "limit must be between 1 and 100",
		})
		result.Error = detail
		return failure, result, nil
	}
	sessions, err := h.service.ListHistory(ctx, owner)
	if err != nil {
		failure, detail := serviceFailure(ctx, "get_workout_history", err)
		result.Error = detail
		return failure, result, nil
	}
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	if sessions != nil {
		result.Sessions = sessions
	}
	return nil, result, nil
}
