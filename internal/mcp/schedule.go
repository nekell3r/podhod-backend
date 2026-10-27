package mcptransport

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"podhod/server/internal/service"
)

type templateWriteInput struct {
	ExpectedRevision int64                 `json:"expected_revision" jsonschema:"Revision from get_training_template."`
	RequestID        string                `json:"request_id" jsonschema:"UUID; reuse only for an identical retry."`
	Days             []service.TrainingDay `json:"days" jsonschema:"Seven days: Monday=1 through Sunday=7."`
}

type weekInput struct {
	WeekStart string `json:"week_start" jsonschema:"Monday date (YYYY-MM-DD)."`
	Timezone  string `json:"timezone" jsonschema:"IANA timezone, e.g. Europe/Moscow."`
}

type weekWriteInput struct {
	WeekStart        string                     `json:"week_start" jsonschema:"Monday date (YYYY-MM-DD)."`
	Timezone         string                     `json:"timezone" jsonschema:"IANA timezone, e.g. Europe/Moscow."`
	ExpectedRevision int64                      `json:"expected_revision" jsonschema:"Week revision; 0 for a new week."`
	RequestID        string                     `json:"request_id" jsonschema:"UUID; reuse only for an identical retry."`
	Workouts         []service.ScheduledWorkout `json:"workouts" jsonschema:"This week; preserve retained IDs."`
}

type moveWorkoutInput struct {
	WeekStart        string `json:"week_start" jsonschema:"The Monday starting the workout's current week, as YYYY-MM-DD."`
	WorkoutID        string `json:"workout_id" jsonschema:"The scheduled workout ID from get_training_week."`
	ToDate           string `json:"to_date" jsonschema:"Unoccupied destination date within the same week, as YYYY-MM-DD."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Current revision returned by get_training_week."`
	RequestID        string `json:"request_id" jsonschema:"UUID; reuse only for an identical retry."`
}

type scheduledRestInput struct {
	WeekStart                string `json:"week_start" jsonschema:"The Monday starting the workout's week, as YYYY-MM-DD."`
	WorkoutID                string `json:"workout_id" jsonschema:"The scheduled workout ID from get_training_week."`
	ExerciseID               string `json:"exercise_id" jsonschema:"Planned exercise ID, not catalog ID."`
	RestSeconds              int    `json:"between_sets_seconds" jsonschema:"Rest between sets in seconds, from 0 to 600."`
	RestAfterExerciseSeconds int    `json:"after_exercise_seconds" jsonschema:"Rest after exercise (0–600 seconds)."`
	ExpectedRevision         int64  `json:"expected_revision" jsonschema:"Current revision returned by get_training_week."`
	RequestID                string `json:"request_id" jsonschema:"UUID; reuse only for an identical retry."`
}

func (h *handler) getTemplate(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	_ emptyInput,
) (*sdkmcp.CallToolResult, templateResult, error) {
	owner, denied := authorizeTool(req, ScopeScheduleRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, templateResult{}, nil
	}
	program, err := h.service.GetTemplate(ctx, owner)
	if err != nil {
		result, detail := serviceFailure(ctx, "get_training_template", err)
		return result, templateResult{Error: detail}, nil
	}
	view, err := h.service.TemplateView(program)
	if err != nil {
		result, detail := serviceFailure(ctx, "template_catalog_volume", err)
		return result, templateResult{Error: detail}, nil
	}
	return nil, templateResult{Template: &view}, nil
}

func (h *handler) replaceTemplate(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input templateWriteInput,
) (*sdkmcp.CallToolResult, templateResult, error) {
	owner, denied := authorizeTool(req, ScopeScheduleWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, templateResult{}, nil
	}
	program, err := h.service.ReplaceTemplate(ctx, owner, service.TemplateWrite{
		ExpectedRevision: input.ExpectedRevision,
		RequestID:        input.RequestID,
		Days:             input.Days,
	})
	if err != nil {
		result, detail := serviceFailure(ctx, "replace_training_template", err)
		return result, templateResult{Error: detail}, nil
	}
	view, err := h.service.TemplateView(program)
	if err != nil {
		result, detail := serviceFailure(ctx, "template_catalog_volume", err)
		return result, templateResult{Error: detail}, nil
	}
	return nil, templateResult{Template: &view}, nil
}

func (h *handler) getWeek(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input weekInput,
) (*sdkmcp.CallToolResult, weekResult, error) {
	owner, denied := authorizeTool(req, ScopeScheduleRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, weekResult{}, nil
	}
	week, err := h.service.GetWeek(
		ctx,
		owner,
		input.WeekStart,
		input.Timezone,
	)
	if err != nil {
		result, detail := serviceFailure(ctx, "get_training_week", err)
		return result, weekResult{Error: detail}, nil
	}
	view, err := h.service.WeekView(week)
	if err != nil {
		result, detail := serviceFailure(ctx, "week_catalog_volume", err)
		return result, weekResult{Error: detail}, nil
	}
	return nil, weekResult{Week: &view}, nil
}

func (h *handler) replaceWeek(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input weekWriteInput,
) (*sdkmcp.CallToolResult, weekResult, error) {
	owner, denied := authorizeTool(req, ScopeScheduleWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, weekResult{}, nil
	}
	week, err := h.service.ReplaceWeek(
		ctx,
		owner,
		input.WeekStart,
		service.WeekWrite{
			ExpectedRevision: input.ExpectedRevision,
			RequestID:        input.RequestID,
			Timezone:         input.Timezone,
			Workouts:         input.Workouts,
		},
	)
	if err != nil {
		result, detail := serviceFailure(ctx, "replace_training_week", err)
		return result, weekResult{Error: detail}, nil
	}
	view, err := h.service.WeekView(week)
	if err != nil {
		result, detail := serviceFailure(ctx, "week_catalog_volume", err)
		return result, weekResult{Error: detail}, nil
	}
	return nil, weekResult{Week: &view}, nil
}

func (h *handler) moveWorkout(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input moveWorkoutInput,
) (*sdkmcp.CallToolResult, weekResult, error) {
	owner, denied := authorizeTool(req, ScopeScheduleWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, weekResult{}, nil
	}
	week, err := h.service.MoveWorkout(
		ctx,
		owner,
		input.WeekStart,
		service.MoveWorkoutRequest{
			ExpectedRevision: input.ExpectedRevision,
			RequestID:        input.RequestID,
			WorkoutID:        input.WorkoutID,
			PlannedDate:      input.ToDate,
		},
	)
	if err != nil {
		result, detail := serviceFailure(ctx, "move_scheduled_workout", err)
		return result, weekResult{Error: detail}, nil
	}
	view, err := h.service.WeekView(week)
	if err != nil {
		result, detail := serviceFailure(ctx, "week_catalog_volume", err)
		return result, weekResult{Error: detail}, nil
	}
	return nil, weekResult{Week: &view}, nil
}

func (h *handler) setScheduledRest(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input scheduledRestInput,
) (*sdkmcp.CallToolResult, weekResult, error) {
	owner, denied := authorizeTool(req, ScopeScheduleWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, weekResult{}, nil
	}
	week, err := h.service.SetScheduledRest(
		ctx,
		owner,
		input.WeekStart,
		service.ScheduledRestRequest{
			ExpectedRevision:         input.ExpectedRevision,
			RequestID:                input.RequestID,
			WorkoutID:                input.WorkoutID,
			ExerciseID:               input.ExerciseID,
			RestSeconds:              input.RestSeconds,
			RestAfterExerciseSeconds: input.RestAfterExerciseSeconds,
		},
	)
	if err != nil {
		result, detail := serviceFailure(ctx, "update_planned_exercise_rest", err)
		return result, weekResult{Error: detail}, nil
	}
	view, err := h.service.WeekView(week)
	if err != nil {
		result, detail := serviceFailure(ctx, "week_catalog_volume", err)
		return result, weekResult{Error: detail}, nil
	}
	return nil, weekResult{Week: &view}, nil
}
