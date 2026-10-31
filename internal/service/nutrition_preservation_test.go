package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type storedDocumentFixture struct {
	resource Resource
}

func (r *storedDocumentFixture) Get(context.Context, string, string) (Resource, error) {
	return r.resource, nil
}

func (r *storedDocumentFixture) Update(
	_ context.Context,
	_ Commit,
	transform func(Resource) (Resource, error),
) (Resource, error) {
	result, err := transform(r.resource)
	if err == nil {
		r.resource = result
	}
	return result, err
}

func (r *storedDocumentFixture) History(context.Context, string) ([]WorkoutSession, error) {
	return []WorkoutSession{}, errors.New("unexpected workout history call")
}

func (r *storedDocumentFixture) AppendHistory(context.Context, string, HistoryWrite) (WorkoutSession, error) {
	return WorkoutSession{}, errors.New("unexpected workout history call")
}

func TestNutritionWriteResultDoesNotAliasCallerOrStoredSnapshots(t *testing.T) {
	t.Parallel()
	repo := &storedDocumentFixture{}
	s := New(repo)
	input := NutritionDayWrite{
		RequestID: "00000000-0000-4000-8000-000000000010", Document: nutritionDayFixture(),
	}
	result, err := s.ReplaceNutritionDay(t.Context(), "owner", "2026-10-31", input)
	if err != nil {
		t.Fatal(err)
	}
	*result.Document.Entries[0].Food.Per100Grams.EnergyKcal = 999
	result.Document.Entries[0].Food.Name = "changed returned snapshot"
	inputEnergyUnchanged := *input.Document.Entries[0].Food.Per100Grams.EnergyKcal == 150
	if !inputEnergyUnchanged || input.Document.Entries[0].Food.Name != "Test food" {
		t.Fatal("returned record aliases the caller's input")
	}
	stored, err := s.GetNutritionDay(t.Context(), "owner", "2026-10-31", "UTC")
	if err != nil || *stored.Document.Entries[0].Food.Per100Grams.EnergyKcal != 150 {
		t.Fatalf("stored snapshot changed: %+v %v", stored, err)
	}
}

func TestNutritionRecordedDayKeepsTimezoneAndGoal(t *testing.T) {
	t.Parallel()
	repo := &storedDocumentFixture{}
	s := New(repo)
	input := NutritionDayWrite{
		RequestID: "00000000-0000-4000-8000-000000000010", Document: nutritionDayFixture(),
	}
	if _, err := s.ReplaceNutritionDay(t.Context(), "owner", input.Document.Date, input); err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision = 1
	input.RequestID = "00000000-0000-4000-8000-000000000011"
	input.Document.Timezone = "UTC"
	_, err := s.ReplaceNutritionDay(t.Context(), "owner", input.Document.Date, input)
	var conflict *Error
	if !errors.As(err, &conflict) || conflict.Code != "nutrition_day_immutable" {
		t.Fatalf("timezone changed without protection: %v", err)
	}
	input.Document.Timezone = "Europe/Moscow"
	input.Document.Goal = &NutritionGoal{
		ID: "00000000-0000-4000-8000-000000000003", EffectiveFrom: "2026-10-01", Timezone: "Europe/Moscow",
		Target: NutritionTotals{EnergyKcal: 2200, ProteinGrams: 130, FatGrams: 70, CarbohydrateGrams: 250}.facts(),
	}
	_, err = s.ReplaceNutritionDay(t.Context(), "owner", input.Document.Date, input)
	if !errors.As(err, &conflict) || conflict.Code != "nutrition_goal_immutable" {
		t.Fatalf("a new goal rewrote an old day without a target: %v", err)
	}
	if repo.resource.Revision != 1 {
		t.Fatal("rejected writes changed the revision")
	}
}

func TestNutritionUnknownOrCorruptStoredDocumentsAreNeverReplaced(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		body  string
		read  func(*Service) error
		write func(*Service) error
	}{
		{name: "day", body: `{"revision":4,"document":{"version":2,"privateFutureField":"keep"}}`,
			read: func(s *Service) error {
				_, err := s.GetNutritionDay(t.Context(), "owner", "2026-10-31", "UTC")
				return err
			},
			write: func(s *Service) error {
				_, err := s.ReplaceNutritionDay(t.Context(), "owner", "2026-10-31", NutritionDayWrite{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010", Document: nutritionDayFixture(),
				})
				return err
			}},
		{name: "goals", body: `{"revision":4,"document":{"version":2,"privateFutureField":"keep"}}`,
			read: func(s *Service) error { _, err := s.GetNutritionGoals(t.Context(), "owner"); return err },
			write: func(s *Service) error {
				_, err := s.ReplaceNutritionGoals(t.Context(), "owner", NutritionGoalsWrite{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010",
					Document: NutritionGoalsDocument{Version: 1, Goals: []NutritionGoal{}},
				})
				return err
			}},
		{name: "recipes", body: `{"revision":4,"document":{"version":2,"privateFutureField":"keep"}}`,
			read: func(s *Service) error { _, err := s.GetNutritionRecipes(t.Context(), "owner"); return err },
			write: func(s *Service) error {
				_, err := s.ReplaceNutritionRecipes(t.Context(), "owner", NutritionRecipesWrite{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010",
					Document: NutritionRecipesDocument{Version: 1, Recipes: []NutritionOwnRecipe{}},
				})
				return err
			}},
		{name: "preferences", body: `{"revision":4,"document":{"version":2,"privateFutureField":"keep"}}`,
			read: func(s *Service) error { _, err := s.GetNutritionPreferences(t.Context(), "owner"); return err },
			write: func(s *Service) error {
				_, err := s.ReplaceNutritionPreferences(t.Context(), "owner", NutritionPreferencesWrite{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010",
					Document: NutritionPreferencesDocument{Version: 1, FavoriteRecipeIDs: []string{}, FavoriteFoodIDs: []string{}},
				})
				return err
			}},
		{name: "corrupt entries",
			body: `{"revision":4,"document":{"version":1,"date":"2026-10-31","timezone":"UTC","entries":null,"goal":null}}`,
			read: func(s *Service) error {
				_, err := s.GetNutritionDay(t.Context(), "owner", "2026-10-31", "UTC")
				return err
			},
			write: func(s *Service) error {
				_, err := s.ReplaceNutritionDay(t.Context(), "owner", "2026-10-31", NutritionDayWrite{
					ExpectedRevision: 4, RequestID: "00000000-0000-4000-8000-000000000010", Document: nutritionDayFixture(),
				})
				return err
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &storedDocumentFixture{resource: Resource{Revision: 4, Payload: json.RawMessage(tc.body)}}
			before := Resource{Revision: 4, Payload: append(json.RawMessage{}, repo.resource.Payload...)}
			s := New(repo)
			if tc.read(s) == nil || tc.write(s) == nil {
				t.Fatal("unsupported or corrupt stored data was accepted")
			}
			if !reflect.DeepEqual(repo.resource, before) {
				t.Fatal("original stored resource was replaced")
			}
		})
	}
}
