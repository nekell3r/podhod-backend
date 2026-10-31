//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"podhod/server/internal/httpapi"
	"podhod/server/internal/service"
)

func nutritionFactsFixture(value service.NutritionTotals) service.NutritionFacts {
	return service.NutritionFacts{
		EnergyKcal: &value.EnergyKcal, ProteinGrams: &value.ProteinGrams,
		FatGrams: &value.FatGrams, CarbohydrateGrams: &value.CarbohydrateGrams,
	}
}

func nutritionOilFixture() service.NutritionFoodSnapshot {
	return service.NutritionFoodSnapshot{
		Name:        "Olive oil snapshot",
		Per100Grams: nutritionFactsFixture(service.NutritionTotals{EnergyKcal: 884, FatGrams: 100}),
		Origin:      service.NutritionOrigin{Kind: "catalogFood", ID: "usda-171413", CatalogVersion: "2026.10.31.2"},
	}
}

func nutritionDayFixture() service.NutritionDayDocument {
	return service.NutritionDayDocument{Version: 1, Date: "2026-10-31", Timezone: "Europe/Moscow",
		Entries: []service.NutritionDiaryEntry{{ID: requestID(1), Meal: "breakfast", Grams: 10, Food: nutritionOilFixture()}},
	}
}

func TestNutritionOwnerIsolationCASIdempotencyAndDeletion(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	input := service.NutritionDayWrite{RequestID: requestID(10), Document: nutritionDayFixture()}
	first, err := s.ReplaceNutritionDay(t.Context(), "owner-a", input.Document.Date, input)
	if err != nil || first.Revision != 1 || math.Abs(first.Totals.EnergyKcal-88.4) > 1e-9 {
		t.Fatalf("first write failed: %+v %v", first, err)
	}
	replay, err := s.ReplaceNutritionDay(t.Context(), "owner-a", input.Document.Date, input)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatalf("idempotent replay changed: %+v %v", replay, err)
	}
	other, err := s.GetNutritionDay(t.Context(), "owner-b", input.Document.Date, "America/New_York")
	if err != nil || other.Revision != 0 || len(other.Document.Entries) != 0 {
		t.Fatalf("foreign nutrition leaked: %+v %v", other, err)
	}
	if _, err := s.ReplaceNutritionDay(t.Context(), "owner-b", input.Document.Date, input); err != nil {
		t.Fatalf("another owner could not reuse its own request ID: %v", err)
	}
	modified := service.NutritionDayWrite{RequestID: input.RequestID, Document: nutritionDayFixture()}
	modified.Document.Entries[0].Grams = 20
	if _, err := s.ReplaceNutritionDay(t.Context(), "owner-a", input.Document.Date, modified); err == nil {
		t.Fatal("request ID accepted for another nutrition command")
	}
	remove := service.NutritionDayWrite{
		ExpectedRevision: 1, RequestID: requestID(11), Document: service.NutritionDayDocument{
			Version: 1, Date: input.Document.Date, Timezone: input.Document.Timezone, Entries: []service.NutritionDiaryEntry{},
		},
	}
	deleted, err := s.ReplaceNutritionDay(t.Context(), "owner-a", input.Document.Date, remove)
	if err != nil || deleted.Revision != 2 || len(deleted.Document.Entries) != 0 || deleted.Totals.EnergyKcal != 0 {
		t.Fatalf("explicit deletion failed: %+v %v", deleted, err)
	}
	stale := service.NutritionDayWrite{ExpectedRevision: 1, RequestID: requestID(12), Document: input.Document}
	if _, err := s.ReplaceNutritionDay(t.Context(), "owner-a", input.Document.Date, stale); err == nil {
		t.Fatal("stale snapshot resurrected a deleted entry")
	}
	if _, err := s.ReplaceNutritionDay(t.Context(), "owner-a", input.Document.Date, input); err != nil {
		t.Fatalf("old exact retry must still return its receipt: %v", err)
	}
	current, err := s.GetNutritionDay(t.Context(), "owner-a", input.Document.Date, "UTC")
	if err != nil || current.Revision != 2 || len(current.Document.Entries) != 0 {
		t.Fatalf("an old receipt rewrote current data: %+v %v", current, err)
	}
	if current.Document.Timezone != "Europe/Moscow" {
		t.Fatal("read query moved the recorded day to another timezone")
	}
}

func TestNutritionConcurrentCASAllowsOneWinner(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for index := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ReplaceNutritionDay(t.Context(), "owner", "2026-10-31", service.NutritionDayWrite{
				RequestID: requestID(byte(20 + index)), Document: nutritionDayFixture(),
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
			continue
		}
		var conflict *service.Error
		if !errors.As(err, &conflict) || conflict.Code != "revision_conflict" || conflict.CurrentRevision != 1 {
			t.Fatalf("unexpected concurrent write error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent winners=%d, expected 1", winners)
	}
}

func TestNutritionRecipeAndGoalEditsDoNotRewriteRecordedDay(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	recipe := service.NutritionOwnRecipe{
		ID: requestID(40), Name: "Original dressing", Category: "salad", Minutes: 5,
		Servings: 1, PreparedWeightGrams: 100,
		Ingredients: []service.NutritionRecipeIngredient{
			{ID: requestID(41), Grams: 10, Food: nutritionOilFixture()},
			{ID: requestID(42), Grams: 90, Food: service.NutritionFoodSnapshot{
				Name: "Water", Origin: service.NutritionOrigin{Kind: "manual"},
				Per100Grams: nutritionFactsFixture(service.NutritionTotals{}),
			}},
		},
		Steps: []string{"Mix and weigh"},
	}
	_, err := s.ReplaceNutritionRecipes(t.Context(), "owner", service.NutritionRecipesWrite{
		RequestID: requestID(43),
		Document: service.NutritionRecipesDocument{Version: 1,
			Recipes: []service.NutritionOwnRecipe{recipe}},
	})
	if err != nil {
		t.Fatal(err)
	}
	goal := service.NutritionGoal{ID: requestID(44), EffectiveFrom: "2026-10-01", Timezone: "Europe/Moscow",
		Target: nutritionFactsFixture(service.NutritionTotals{
			EnergyKcal: 2200, ProteinGrams: 130, FatGrams: 70, CarbohydrateGrams: 250,
		}),
	}
	if _, err := s.ReplaceNutritionGoals(t.Context(), "owner", service.NutritionGoalsWrite{
		RequestID: requestID(45), Document: service.NutritionGoalsDocument{Version: 1, Goals: []service.NutritionGoal{goal}},
	}); err != nil {
		t.Fatal(err)
	}
	calculation, err := recipe.Calculate()
	if err != nil {
		t.Fatal(err)
	}
	day := nutritionDayFixture()
	day.Goal = &goal
	day.Entries[0].Food = service.NutritionFoodSnapshot{
		Name: recipe.Name, Per100Grams: nutritionFactsFixture(calculation.Per100Grams),
		Origin: service.NutritionOrigin{Kind: "ownRecipe", ID: recipe.ID},
	}
	saved, err := s.ReplaceNutritionDay(t.Context(), "owner", day.Date, service.NutritionDayWrite{
		RequestID: requestID(46), Document: day,
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedRecipe := recipe
	updatedRecipe.Name = "Changed recipe"
	updatedRecipe.Ingredients = append([]service.NutritionRecipeIngredient{}, recipe.Ingredients...)
	updatedRecipe.Ingredients[0].Grams = 20
	updatedRecipe.Ingredients[1].Grams = 80
	if _, err := s.ReplaceNutritionRecipes(t.Context(), "owner", service.NutritionRecipesWrite{
		ExpectedRevision: 1, RequestID: requestID(47),
		Document: service.NutritionRecipesDocument{Version: 1, Recipes: []service.NutritionOwnRecipe{updatedRecipe}},
	}); err != nil {
		t.Fatal(err)
	}
	updatedGoal := goal
	updatedGoal.Target = nutritionFactsFixture(service.NutritionTotals{
		EnergyKcal: 2600, ProteinGrams: 160, FatGrams: 90, CarbohydrateGrams: 300,
	})
	if _, err := s.ReplaceNutritionGoals(t.Context(), "owner", service.NutritionGoalsWrite{
		ExpectedRevision: 1, RequestID: requestID(48),
		Document: service.NutritionGoalsDocument{Version: 1, Goals: []service.NutritionGoal{updatedGoal}},
	}); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetNutritionDay(t.Context(), "owner", day.Date, "UTC")
	if err != nil || !reflect.DeepEqual(current, saved) {
		t.Fatalf("recorded snapshot changed with recipe/goal: %+v %v", current, err)
	}
	changedDay := day
	changedDay.Goal = &updatedGoal
	if _, err := s.ReplaceNutritionDay(t.Context(), "owner", day.Date, service.NutritionDayWrite{
		ExpectedRevision: 1, RequestID: requestID(49), Document: changedDay,
	}); err == nil {
		t.Fatal("a new goal silently rewrote an existing day's target")
	}
}

func TestNutritionHTTPUsesVerifiedOwnerAndRejectsMissingFactsWithoutSaving(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	if _, err := s.ReplaceNutritionDay(t.Context(), "owner-a", "2026-10-31", service.NutritionDayWrite{
		RequestID: requestID(60), Document: nutritionDayFixture(),
	}); err != nil {
		t.Fatal(err)
	}
	verifier := func(_ context.Context, raw string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		return &sdkauth.TokenInfo{
			UserID: raw, Scopes: []string{"workout:read", "workout:write"}, Expiration: time.Now().Add(time.Hour),
		}, nil
	}
	handler := sdkauth.RequireBearerToken(verifier, nil)(httpapi.New(s))
	request := httptest.NewRequest(http.MethodGet, "/v1/nutrition/days/2026-10-31?owner=owner-a", nil)
	request.Header.Set("Authorization", "Bearer owner-b")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var day service.NutritionDayView
	if err := json.Unmarshal(response.Body.Bytes(), &day); err != nil || response.Code != 200 {
		t.Fatalf("GET response=%s error=%v", response.Body.String(), err)
	}
	if day.Revision != 0 || len(day.Document.Entries) != 0 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("owner query affected identity or private cache policy")
	}
	for _, path := range []string{"goals", "recipes", "preferences"} {
		request = httptest.NewRequest(http.MethodGet, "/v1/nutrition/"+path, nil)
		request.Header.Set("Authorization", "Bearer owner-b")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("empty %s HTTP %d: %s", path, response.Code, response.Body.String())
		}
	}
	input := service.NutritionDayWrite{RequestID: requestID(61), Document: nutritionDayFixture()}
	input.Document.Entries[0].Food.Per100Grams.ProteinGrams = nil
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPut, "/v1/nutrition/days/2026-10-31", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer owner-b")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatalf("null nutrient HTTP %d: %s", response.Code, response.Body.String())
	}
	unchanged, err := s.GetNutritionDay(t.Context(), "owner-b", "2026-10-31", "UTC")
	if err != nil || unchanged.Revision != 0 {
		t.Fatal("invalid nutrition write created a resource")
	}
}
