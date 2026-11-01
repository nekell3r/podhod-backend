package mcptransport_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	mcptransport "podhod/server/internal/mcp"
	"podhod/server/internal/service"
)

func nutritionRecipe() service.NutritionOwnRecipe {
	energy, protein, fat, carbohydrate := 150.0, 10.0, 10.0, 5.0
	return service.NutritionOwnRecipe{
		ID: "70000000-0000-4000-8000-000000000001", Name: "Own breakfast", Category: "breakfast",
		Minutes: 10, Servings: 2, PreparedWeightGrams: 200,
		Ingredients: []service.NutritionRecipeIngredient{{
			ID: "70000000-0000-4000-8000-000000000002", Grams: 200,
			Food: service.NutritionFoodSnapshot{Name: "Label food", Origin: service.NutritionOrigin{Kind: "manual"},
				Per100Grams: service.NutritionFacts{
					EnergyKcal: &energy, ProteinGrams: &protein, FatGrams: &fat, CarbohydrateGrams: &carbohydrate,
				}},
		}},
		Steps: []string{"Prepare and weigh"},
	}
}

func nutritionLogInput() map[string]any {
	recipe := nutritionRecipe()
	food := recipe.Ingredients[0].Food
	food.Name = recipe.Name
	food.Origin = service.NutritionOrigin{Kind: "ownRecipe", ID: recipe.ID}
	return map[string]any{
		"date": "2026-11-01", "timezone": "Europe/Moscow", "initial_goal": nil,
		"expected_revision": 0, "request_id": "70000000-0000-4000-8000-000000000004",
		"entry": service.NutritionDiaryEntry{
			ID: "70000000-0000-4000-8000-000000000003", Meal: "breakfast", Grams: 150, Food: food,
		},
	}
}

type nutritionRecipesResponse struct {
	Recipes        service.NutritionRecipesRecord `json:"recipes"`
	DeliveryStatus string                         `json:"delivery_status"`
	Error          *failure                       `json:"error"`
}

type nutritionDayResponse struct {
	Day            service.NutritionDayView `json:"day"`
	DeliveryStatus string                   `json:"delivery_status"`
	Error          *failure                 `json:"error"`
}

func TestNutritionMCPRecipeAndDiaryLifecycle(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	svc := service.New(repo)
	client := connect(t, svc, identity{owner: "owner-a", scopes: []string{
		mcptransport.ScopeWorkoutRead, mcptransport.ScopeWorkoutWrite,
	}})
	for _, name := range []string{"get_nutrition_recipes", "get_nutrition_goals", "get_nutrition_catalog"} {
		result := callTool(t, client, name, map[string]any{})
		if result.IsError {
			t.Fatalf("%s: %+v", name, result)
		}
		if name == "get_nutrition_catalog" {
			catalog := decodeResult[struct {
				Catalog struct {
					Foods   []json.RawMessage `json:"foods"`
					Recipes []json.RawMessage `json:"recipes"`
				} `json:"catalog"`
			}](t, result)
			if len(catalog.Catalog.Foods) != 24 || len(catalog.Catalog.Recipes) != 16 {
				t.Fatalf("catalog lost items: %+v", catalog.Catalog)
			}
		}
	}
	dayRead := map[string]any{"date": "2026-11-01", "timezone": "Europe/Moscow"}
	if result := callTool(t, client, "get_nutrition_day", dayRead); result.IsError || repo.writeCount() != 0 {
		t.Fatalf("reads changed data or failed: %+v", result)
	}
	create := map[string]any{"recipe": nutritionRecipe(), "expected_revision": 0,
		"request_id": "70000000-0000-4000-8000-000000000005"}
	result := callTool(t, client, "create_nutrition_recipe", create)
	created := decodeResult[nutritionRecipesResponse](t, result)
	if result.IsError || created.Recipes.Revision != 1 || len(created.Recipes.Document.Recipes) != 1 || created.DeliveryStatus != "pending_sync" {
		t.Fatalf("create recipe: %+v %+v", result, created)
	}
	log := nutritionLogInput()
	loggedResult := callTool(t, client, "log_nutrition_entry", log)
	logged := decodeResult[nutritionDayResponse](t, loggedResult)
	if loggedResult.IsError || logged.Day.Revision != 1 || logged.Day.Totals.EnergyKcal != 225 || logged.DeliveryStatus != "pending_sync" {
		t.Fatalf("log food: %+v %+v", loggedResult, logged)
	}
	retry := callTool(t, client, "log_nutrition_entry", log)
	if retry.IsError || !reflect.DeepEqual(logged, decodeResult[nutritionDayResponse](t, retry)) || repo.writeCount() != 2 {
		t.Fatalf("retry duplicated food: %+v", retry)
	}
	second := nutritionRecipe()
	second.ID = "70000000-0000-4000-8000-000000000006"
	create["recipe"], create["expected_revision"], create["request_id"] = second, 1, "70000000-0000-4000-8000-000000000007"
	if result := callTool(t, client, "create_nutrition_recipe", create); result.IsError {
		t.Fatalf("create second recipe: %+v", result)
	}
	deleteInput := map[string]any{"recipe_id": nutritionRecipe().ID, "expected_revision": 2,
		"request_id": "70000000-0000-4000-8000-000000000008"}
	deletedResult := callTool(t, client, "delete_nutrition_recipe", deleteInput)
	deleted := decodeResult[nutritionRecipesResponse](t, deletedResult)
	if deletedResult.IsError || deleted.Recipes.Revision != 3 || len(deleted.Recipes.Document.Recipes) != 1 || deleted.Recipes.Document.Recipes[0].ID != second.ID {
		t.Fatalf("delete changed another recipe: %+v %+v", deletedResult, deleted)
	}
	if retry := callTool(t, client, "delete_nutrition_recipe", deleteInput); retry.IsError || repo.writeCount() != 4 {
		t.Fatalf("delete retry was not idempotent: %+v", retry)
	}
	oldCreate := map[string]any{"recipe": nutritionRecipe(), "expected_revision": 0,
		"request_id": "70000000-0000-4000-8000-000000000005"}
	if receipt := callTool(t, client, "create_nutrition_recipe", oldCreate); receipt.IsError {
		t.Fatalf("old receipt failed: %+v", receipt)
	}
	current, _ := svc.GetNutritionRecipes(t.Context(), "owner-a")
	if current.Revision != 3 || len(current.Document.Recipes) != 1 || current.Document.Recipes[0].ID != second.ID {
		t.Fatal("old create receipt restored deleted recipe")
	}
	after := decodeResult[nutritionDayResponse](t, callTool(t, client, "get_nutrition_day", dayRead))
	if !reflect.DeepEqual(after.Day, logged.Day) {
		t.Fatal("deleting recipe changed recorded diary snapshot")
	}
	other := connect(t, svc, identity{owner: "owner-b", scopes: []string{mcptransport.ScopeWorkoutRead}})
	otherRecipes := decodeResult[nutritionRecipesResponse](t, callTool(t, other, "get_nutrition_recipes", map[string]any{}))
	otherDay := decodeResult[nutritionDayResponse](t, callTool(t, other, "get_nutrition_day", dayRead))
	if otherRecipes.Recipes.Revision != 0 || len(otherRecipes.Recipes.Document.Recipes) != 0 || len(otherDay.Day.Document.Entries) != 0 {
		t.Fatal("nutrition data leaked between accounts")
	}
}

func TestNutritionMCPUpdatesOneRecipeWithoutRewritingDiary(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	svc := service.New(repo)
	original := nutritionRecipe()
	other := nutritionRecipe()
	other.ID = "70000000-0000-4000-8000-000000000006"
	if _, err := svc.ReplaceNutritionRecipes(t.Context(), "owner", service.NutritionRecipesWrite{
		RequestID: sessionID, Document: service.NutritionRecipesDocument{Version: 1, Recipes: []service.NutritionOwnRecipe{original, other}},
	}); err != nil {
		t.Fatal(err)
	}
	client := connect(t, svc, identity{owner: "owner", scopes: []string{mcptransport.ScopeWorkoutRead, mcptransport.ScopeWorkoutWrite}})
	loggedResult := callTool(t, client, "log_nutrition_entry", nutritionLogInput())
	if loggedResult.IsError {
		t.Fatalf("seed diary: %+v", loggedResult)
	}
	logged := decodeResult[nutritionDayResponse](t, loggedResult)
	edited := nutritionRecipe()
	edited.Name = "Updated recipe"
	edited.Minutes = 20
	edited.Category = "main"
	edited.Servings = 1
	edited.PreparedWeightGrams = 100
	edited.Ingredients[0].Grams = 100
	edited.Steps = []string{"Changed step", "Serve"}
	input := map[string]any{"recipe_id": original.ID, "recipe": edited, "expected_revision": 1,
		"request_id": "70000000-0000-4000-8000-000000000011"}
	result := callTool(t, client, "update_nutrition_recipe", input)
	updated := decodeResult[nutritionRecipesResponse](t, result)
	if result.IsError || updated.Recipes.Revision != 2 || updated.DeliveryStatus != "pending_sync" ||
		!reflect.DeepEqual(updated.Recipes.Document.Recipes, []service.NutritionOwnRecipe{edited, other}) {
		t.Fatalf("update changed the wrong recipe or lost fields: %+v %+v", result, updated)
	}
	retry := callTool(t, client, "update_nutrition_recipe", input)
	if retry.IsError || !reflect.DeepEqual(updated, decodeResult[nutritionRecipesResponse](t, retry)) || repo.writeCount() != 3 {
		t.Fatalf("update retry was not idempotent: %+v", retry)
	}
	edited.Name = "Changed retry"
	input["recipe"] = edited
	reused := callTool(t, client, "update_nutrition_recipe", input)
	if !reused.IsError || decodeResult[nutritionRecipesResponse](t, reused).Error.Code != "idempotency_conflict" {
		t.Fatalf("changed retry reused request ID: %+v", reused)
	}
	input["request_id"] = "70000000-0000-4000-8000-000000000012"
	stale := callTool(t, client, "update_nutrition_recipe", input)
	detail := decodeResult[nutritionRecipesResponse](t, stale).Error
	if !stale.IsError || detail == nil || detail.Code != "revision_conflict" || detail.CurrentRevision == nil || *detail.CurrentRevision != 2 {
		t.Fatalf("stale edit overwrote a newer recipe: %+v", stale)
	}
	input["expected_revision"] = 2
	for _, tc := range []struct {
		name     string
		recipeID string
		recipe   service.NutritionOwnRecipe
		code     string
	}{
		{name: "changed UUID", recipeID: other.ID, recipe: edited, code: "invalid_request"},
		{name: "missing recipe", recipeID: "70000000-0000-4000-8000-000000000099",
			recipe: func() service.NutritionOwnRecipe {
				r := edited
				r.ID = "70000000-0000-4000-8000-000000000099"
				return r
			}(), code: "not_found"},
		{name: "invalid ready mass", recipeID: edited.ID,
			recipe: func() service.NutritionOwnRecipe { r := edited; r.PreparedWeightGrams = 0; return r }(), code: "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input["recipe_id"], input["recipe"] = tc.recipeID, tc.recipe
			failure := callTool(t, client, "update_nutrition_recipe", input)
			if !failure.IsError || decodeResult[nutritionRecipesResponse](t, failure).Error.Code != tc.code {
				t.Fatalf("invalid edit accepted: %+v", failure)
			}
		})
	}
	foreign := connect(t, svc, identity{owner: "someone-else", scopes: []string{mcptransport.ScopeWorkoutWrite}})
	input["recipe_id"], input["recipe"], input["expected_revision"] = original.ID, edited, 0
	denied := callTool(t, foreign, "update_nutrition_recipe", input)
	if !denied.IsError || decodeResult[nutritionRecipesResponse](t, denied).Error.Code != "not_found" {
		t.Fatalf("another account edited the recipe: %+v", denied)
	}
	day, err := svc.GetNutritionDay(t.Context(), "owner", "2026-11-01", "Europe/Moscow")
	if err != nil || !reflect.DeepEqual(day, logged.Day) || repo.writeCount() != 3 {
		t.Fatalf("recipe editing changed diary or rejected edits wrote data: %+v %v", day, err)
	}
}

func TestNutritionMCPScopesAndAnnotations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		scope string
		input any
		write bool
	}{
		{name: "get_nutrition_recipes", scope: mcptransport.ScopeWorkoutRead, input: map[string]any{}},
		{name: "get_nutrition_goals", scope: mcptransport.ScopeWorkoutRead, input: map[string]any{}},
		{name: "get_nutrition_catalog", scope: mcptransport.ScopeWorkoutRead, input: map[string]any{}},
		{name: "get_nutrition_day", scope: mcptransport.ScopeWorkoutRead,
			input: map[string]any{"date": "2026-11-01", "timezone": "UTC"}},
		{name: "create_nutrition_recipe", scope: mcptransport.ScopeWorkoutWrite, write: true,
			input: map[string]any{"recipe": nutritionRecipe(), "expected_revision": 0, "request_id": sessionID}},
		{name: "delete_nutrition_recipe", scope: mcptransport.ScopeWorkoutWrite, write: true,
			input: map[string]any{"recipe_id": nutritionRecipe().ID, "expected_revision": 0, "request_id": sessionID}},
		{name: "update_nutrition_recipe", scope: mcptransport.ScopeWorkoutWrite, write: true,
			input: map[string]any{"recipe_id": nutritionRecipe().ID, "recipe": nutritionRecipe(), "expected_revision": 0, "request_id": sessionID}},
		{name: "log_nutrition_entry", scope: mcptransport.ScopeWorkoutWrite, write: true, input: nutritionLogInput()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryRepository()
			wrongScope := mcptransport.ScopeWorkoutWrite
			if tc.write {
				wrongScope = mcptransport.ScopeWorkoutRead
			}
			client := connect(t, service.New(repo), identity{owner: "owner", scopes: []string{wrongScope}})
			result := callTool(t, client, tc.name, tc.input)
			if !result.IsError || !strings.Contains(result.Meta["mcp/www_authenticate"].([]any)[0].(string), tc.scope) || repo.writeCount() != 0 {
				t.Fatalf("scope not enforced: %+v", result)
			}
			tools, err := client.ListTools(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, tool := range tools.Tools {
				if tool.Name != tc.name {
					continue
				}
				found = true
				if tool.Annotations.ReadOnlyHint == tc.write || (tc.write && !tool.Annotations.IdempotentHint) {
					t.Fatalf("incorrect annotations: %+v", tool.Annotations)
				}
				if *tool.Annotations.DestructiveHint != (tc.name == "delete_nutrition_recipe" || tc.name == "update_nutrition_recipe") {
					t.Fatal("incorrect destructive annotation")
				}
			}
			if !found {
				t.Fatal("missing nutrition tool")
			}
		})
	}
}

func TestNutritionMCPConflictsValidationAndClosedInput(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	svc := service.New(repo)
	client := connect(t, svc, identity{owner: "owner", scopes: []string{mcptransport.ScopeWorkoutWrite}})
	log := nutritionLogInput()
	if result := callTool(t, client, "log_nutrition_entry", log); result.IsError {
		t.Fatalf("seed day: %+v", result)
	}
	entry := log["entry"].(service.NutritionDiaryEntry)
	entry.Grams = 175
	log["entry"] = entry
	reused := callTool(t, client, "log_nutrition_entry", log)
	if !reused.IsError || decodeResult[nutritionDayResponse](t, reused).Error.Code != "idempotency_conflict" {
		t.Fatalf("request ID reused for changed food: %+v", reused)
	}
	log = nutritionLogInput()
	log["request_id"] = "70000000-0000-4000-8000-000000000009"
	conflict := callTool(t, client, "log_nutrition_entry", log)
	detail := decodeResult[nutritionDayResponse](t, conflict).Error
	if !conflict.IsError || detail == nil || detail.Code != "revision_conflict" || detail.CurrentRevision == nil || *detail.CurrentRevision != 1 {
		t.Fatalf("stale write silently replaced data: %+v", conflict)
	}
	log["expected_revision"] = 1
	duplicate := callTool(t, client, "log_nutrition_entry", log)
	if !duplicate.IsError || decodeResult[nutritionDayResponse](t, duplicate).Error.Code != "invalid_request" {
		t.Fatalf("duplicate entry accepted: %+v", duplicate)
	}
	entry = log["entry"].(service.NutritionDiaryEntry)
	entry.ID = "70000000-0000-4000-8000-000000000010"
	entry.Food.Per100Grams.ProteinGrams = nil
	log["entry"] = entry
	if result := callTool(t, client, "log_nutrition_entry", log); !result.IsError {
		t.Fatal("unknown nutrition became zero")
	}
	log = nutritionLogInput()
	log["owner"] = "someone-else"
	if result := callTool(t, client, "log_nutrition_entry", log); !result.IsError {
		t.Fatal("owner override accepted")
	}
	if repo.writeCount() != 1 {
		t.Fatal("rejected calls changed data")
	}
}
