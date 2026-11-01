package mcptransport

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"podhod/server/internal/service"
)

type nutritionDayInput struct {
	Date     string `json:"date" jsonschema:"Calendar date YYYY-MM-DD, resolved with the user."`
	Timezone string `json:"timezone" jsonschema:"IANA timezone; use the recorded day's timezone when it exists."`
}

type nutritionRecipeCreateInput struct {
	Recipe           service.NutritionOwnRecipe `json:"recipe" jsonschema:"New UUID and complete ingredient snapshots; preserve explicit per-100-g values and ready weight."`
	ExpectedRevision int64                      `json:"expected_revision" jsonschema:"Revision from get_nutrition_recipes."`
	RequestID        string                     `json:"request_id" jsonschema:"UUID; reuse only for an identical retry."`
}

type nutritionRecipeDeleteInput struct {
	RecipeID         string `json:"recipe_id" jsonschema:"UUID of the own recipe to delete, from get_nutrition_recipes."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Revision from get_nutrition_recipes."`
	RequestID        string `json:"request_id" jsonschema:"UUID; reuse only for an identical retry."`
}

type nutritionEntryInput struct {
	Date             string                      `json:"date" jsonschema:"Calendar date YYYY-MM-DD, resolved with the user."`
	Timezone         string                      `json:"timezone" jsonschema:"IANA timezone; keep the recorded day's timezone."`
	Entry            service.NutritionDiaryEntry `json:"entry" jsonschema:"New UUID, breakfast/lunch/dinner/snack, eaten grams and food snapshot with explicit per-100-g KBJU. Unknown values must not become zero."`
	InitialGoal      *service.NutritionGoal      `json:"initial_goal" jsonschema:"For revision 0: applicable goal snapshot from get_nutrition_goals or explicit null when none. Ignored for an existing day; its saved goal is preserved."`
	ExpectedRevision int64                       `json:"expected_revision" jsonschema:"Revision from get_nutrition_day; zero for an unrecorded day."`
	RequestID        string                      `json:"request_id" jsonschema:"UUID; reuse only for an identical retry to avoid recording a meal twice."`
}

type nutritionRecipesResult struct {
	Recipes        *service.NutritionRecipesRecord `json:"recipes,omitempty"`
	DeliveryStatus string                          `json:"delivery_status,omitempty"`
	Notice         string                          `json:"notice,omitempty"`
	Error          *toolError                      `json:"error,omitempty"`
}

type nutritionDayResult struct {
	Day            *service.NutritionDayView `json:"day,omitempty"`
	DeliveryStatus string                    `json:"delivery_status,omitempty"`
	Notice         string                    `json:"notice,omitempty"`
	Error          *toolError                `json:"error,omitempty"`
}

type nutritionGoalsResult struct {
	Goals *service.NutritionGoalsRecord `json:"goals,omitempty"`
	Error *toolError                    `json:"error,omitempty"`
}

type nutritionCatalogResult struct {
	Catalog map[string]any `json:"catalog,omitempty"`
	Error   *toolError     `json:"error,omitempty"`
}

func (h *handler) getNutritionRecipes(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	_ emptyInput,
) (*sdkmcp.CallToolResult, nutritionRecipesResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, nutritionRecipesResult{}, nil
	}
	record, err := h.service.GetNutritionRecipes(ctx, owner)
	if err != nil {
		result, detail := serviceFailure(ctx, "get_nutrition_recipes", err)
		return result, nutritionRecipesResult{Error: detail}, nil
	}
	return nil, nutritionRecipesResult{Recipes: &record}, nil
}

func (h *handler) createNutritionRecipe(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input nutritionRecipeCreateInput,
) (*sdkmcp.CallToolResult, nutritionRecipesResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, nutritionRecipesResult{}, nil
	}
	record, err := h.service.CreateNutritionRecipe(ctx, owner, service.CreateNutritionRecipeRequest{
		ExpectedRevision: input.ExpectedRevision, RequestID: input.RequestID, Recipe: input.Recipe,
	})
	if err != nil {
		result, detail := serviceFailure(ctx, "create_nutrition_recipe", err)
		return result, nutritionRecipesResult{Error: detail}, nil
	}
	return nil, nutritionRecipesResult{Recipes: &record, DeliveryStatus: "pending_sync", Notice: pendingSyncNotice}, nil
}

func (h *handler) deleteNutritionRecipe(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input nutritionRecipeDeleteInput,
) (*sdkmcp.CallToolResult, nutritionRecipesResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, nutritionRecipesResult{}, nil
	}
	record, err := h.service.DeleteNutritionRecipe(ctx, owner, service.DeleteNutritionRecipeRequest{
		ExpectedRevision: input.ExpectedRevision, RequestID: input.RequestID, RecipeID: input.RecipeID,
	})
	if err != nil {
		result, detail := serviceFailure(ctx, "delete_nutrition_recipe", err)
		return result, nutritionRecipesResult{Error: detail}, nil
	}
	return nil, nutritionRecipesResult{Recipes: &record, DeliveryStatus: "pending_sync", Notice: pendingSyncNotice}, nil
}

func (h *handler) getNutritionDay(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input nutritionDayInput,
) (*sdkmcp.CallToolResult, nutritionDayResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, nutritionDayResult{}, nil
	}
	record, err := h.service.GetNutritionDay(ctx, owner, input.Date, input.Timezone)
	if err != nil {
		result, detail := serviceFailure(ctx, "get_nutrition_day", err)
		return result, nutritionDayResult{Error: detail}, nil
	}
	return nil, nutritionDayResult{Day: &record}, nil
}

func (h *handler) logNutritionEntry(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	input nutritionEntryInput,
) (*sdkmcp.CallToolResult, nutritionDayResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutWrite, h.resourceMetadataURL)
	if denied != nil {
		return denied, nutritionDayResult{}, nil
	}
	record, err := h.service.LogNutritionEntry(ctx, owner, input.Date, service.LogNutritionEntryRequest{
		ExpectedRevision: input.ExpectedRevision, RequestID: input.RequestID, Timezone: input.Timezone,
		InitialGoal: input.InitialGoal, Entry: input.Entry,
	})
	if err != nil {
		result, detail := serviceFailure(ctx, "log_nutrition_entry", err)
		return result, nutritionDayResult{Error: detail}, nil
	}
	return nil, nutritionDayResult{Day: &record, DeliveryStatus: "pending_sync", Notice: pendingSyncNotice}, nil
}

func (h *handler) getNutritionGoals(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	_ emptyInput,
) (*sdkmcp.CallToolResult, nutritionGoalsResult, error) {
	owner, denied := authorizeTool(req, ScopeWorkoutRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, nutritionGoalsResult{}, nil
	}
	record, err := h.service.GetNutritionGoals(ctx, owner)
	if err != nil {
		result, detail := serviceFailure(ctx, "get_nutrition_goals", err)
		return result, nutritionGoalsResult{Error: detail}, nil
	}
	return nil, nutritionGoalsResult{Goals: &record}, nil
}

func (h *handler) getNutritionCatalog(
	ctx context.Context,
	req *sdkmcp.CallToolRequest,
	_ emptyInput,
) (*sdkmcp.CallToolResult, nutritionCatalogResult, error) {
	_, denied := authorizeTool(req, ScopeWorkoutRead, h.resourceMetadataURL)
	if denied != nil {
		return denied, nutritionCatalogResult{}, nil
	}
	var result nutritionCatalogResult
	if err := json.Unmarshal(service.BundledNutritionCatalog(), &result.Catalog); err != nil {
		failure, detail := serviceFailure(ctx, "get_nutrition_catalog", err)
		return failure, nutritionCatalogResult{Error: detail}, nil
	}
	return nil, result, nil
}
