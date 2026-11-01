package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNutritionAppendKeepsRecordedGoalTimezoneAndEntries(t *testing.T) {
	t.Parallel()
	repo := &storedDocumentFixture{}
	svc := New(repo)
	goal := &NutritionGoal{
		ID: "00000000-0000-4000-8000-000000000011", EffectiveFrom: "2026-10-01", Timezone: "Europe/Moscow",
		Target: NutritionTotals{EnergyKcal: 2200, ProteinGrams: 130, FatGrams: 70, CarbohydrateGrams: 250}.facts(),
	}
	input := LogNutritionEntryRequest{RequestID: "00000000-0000-4000-8000-000000000010",
		Timezone: "Europe/Moscow", InitialGoal: goal, Entry: nutritionDayFixture().Entries[0]}
	first, err := svc.LogNutritionEntry(t.Context(), "owner", "2026-10-31", input)
	if err != nil || !reflect.DeepEqual(first.Document.Goal, goal) {
		t.Fatalf("initial goal not saved: %+v %v", first, err)
	}
	input.ExpectedRevision = 1
	input.RequestID = "00000000-0000-4000-8000-000000000012"
	input.Entry.ID = "00000000-0000-4000-8000-000000000013"
	input.InitialGoal = nil
	second, err := svc.LogNutritionEntry(t.Context(), "owner", "2026-10-31", input)
	if err != nil || len(second.Document.Entries) != 2 || !reflect.DeepEqual(second.Document.Goal, first.Document.Goal) ||
		!reflect.DeepEqual(second.Document.Entries[0], first.Document.Entries[0]) {
		t.Fatalf("append rewrote prior snapshots: %+v %v", second, err)
	}
	*second.Document.Entries[1].Food.Per100Grams.EnergyKcal = 999
	if *input.Entry.Food.Per100Grams.EnergyKcal != 150 {
		t.Fatal("result aliases input")
	}
	before := append(json.RawMessage{}, repo.resource.Payload...)
	input.ExpectedRevision = 2
	input.Timezone = "UTC"
	_, err = svc.LogNutritionEntry(t.Context(), "owner", "2026-10-31", input)
	var domain *Error
	if !errors.As(err, &domain) || domain.Code != "nutrition_day_immutable" || !reflect.DeepEqual(before, repo.resource.Payload) {
		t.Fatalf("timezone rewrite not rejected atomically: %v", err)
	}
}

func TestNutritionCommandsRejectCapacityWithoutDroppingExistingItems(t *testing.T) {
	t.Parallel()
	day := nutritionDayFixture()
	for i := 1; i < 100; i++ {
		entry := day.Entries[0]
		entry.ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", i)
		day.Entries = append(day.Entries, entry)
	}
	recipes := NutritionRecipesDocument{Version: 1, Recipes: []NutritionOwnRecipe{}}
	for i := range 100 {
		recipe := nutritionRecipeFixture()
		recipe.ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", i)
		recipes.Recipes = append(recipes.Recipes, recipe)
	}
	for _, tc := range []struct {
		name   string
		stored any
		run    func(*Service) error
	}{
		{name: "full day", stored: NutritionDayRecord{Revision: 5, Document: day},
			run: func(s *Service) error {
				entry := nutritionDayFixture().Entries[0]
				entry.ID = "20000000-0000-4000-8000-000000000001"
				_, err := s.LogNutritionEntry(t.Context(), "owner", day.Date, LogNutritionEntryRequest{
					ExpectedRevision: 5, RequestID: "00000000-0000-4000-8000-000000000010",
					Timezone: day.Timezone, Entry: entry,
				})
				return err
			}},
		{name: "full recipes", stored: NutritionRecipesRecord{Revision: 5, Document: recipes},
			run: func(s *Service) error {
				_, err := s.CreateNutritionRecipe(t.Context(), "owner", CreateNutritionRecipeRequest{
					ExpectedRevision: 5, RequestID: "00000000-0000-4000-8000-000000000010", Recipe: nutritionRecipeFixture(),
				})
				return err
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.stored)
			if err != nil {
				t.Fatal(err)
			}
			repo := &storedDocumentFixture{resource: Resource{Revision: 5, Payload: data}}
			if err := tc.run(New(repo)); err == nil || !strings.Contains(err.Error(), "at most 100") {
				t.Fatalf("expected capacity error: %v", err)
			}
			if repo.resource.Revision != 5 || string(repo.resource.Payload) != string(data) {
				t.Fatal("capacity rejection dropped prior items")
			}
		})
	}
}

func TestNutritionCommandsProtectFutureAndCorruptStorage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		run  func(*Service) error
	}{
		{name: "future recipe create", body: `{"revision":4,"document":{"version":2,"future":"keep"}}`,
			run: func(s *Service) error {
				_, err := s.CreateNutritionRecipe(t.Context(), "owner", CreateNutritionRecipeRequest{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010", Recipe: nutritionRecipeFixture(),
				})
				return err
			}},
		{name: "future recipe delete", body: `{"revision":4,"document":{"version":2,"future":"keep"}}`,
			run: func(s *Service) error {
				_, err := s.DeleteNutritionRecipe(t.Context(), "owner", DeleteNutritionRecipeRequest{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010", RecipeID: nutritionRecipeFixture().ID,
				})
				return err
			}},
		{name: "future recipe update", body: `{"revision":4,"document":{"version":2,"future":"keep"}}`,
			run: func(s *Service) error {
				_, err := s.UpdateNutritionRecipe(t.Context(), "owner", UpdateNutritionRecipeRequest{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010",
					RecipeID: nutritionRecipeFixture().ID, Recipe: nutritionRecipeFixture(),
				})
				return err
			}},
		{name: "corrupt recipe update", body: `{"revision":4,"document":{"version":1}}`,
			run: func(s *Service) error {
				_, err := s.UpdateNutritionRecipe(t.Context(), "owner", UpdateNutritionRecipeRequest{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010",
					RecipeID: nutritionRecipeFixture().ID, Recipe: nutritionRecipeFixture(),
				})
				return err
			}},
		{name: "future diary append", body: `{"revision":4,"document":{"version":2,"future":"keep"}}`,
			run: func(s *Service) error {
				_, err := s.LogNutritionEntry(t.Context(), "owner", "2026-10-31", LogNutritionEntryRequest{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010", Timezone: "Europe/Moscow", Entry: nutritionDayFixture().Entries[0],
				})
				return err
			}},
		{name: "corrupt diary append", body: `{"revision":4,"document":{"version":1}}`,
			run: func(s *Service) error {
				_, err := s.LogNutritionEntry(t.Context(), "owner", "2026-10-31", LogNutritionEntryRequest{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010", Timezone: "Europe/Moscow", Entry: nutritionDayFixture().Entries[0],
				})
				return err
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &storedDocumentFixture{resource: Resource{Revision: 4, Payload: json.RawMessage(tc.body)}}
			before := append(json.RawMessage{}, repo.resource.Payload...)
			if err := tc.run(New(repo)); err == nil {
				t.Fatal("unsupported stored data was overwritten")
			}
			if repo.resource.Revision != 4 || !reflect.DeepEqual(before, repo.resource.Payload) {
				t.Fatal("rejected command changed stored bytes")
			}
		})
	}
}
