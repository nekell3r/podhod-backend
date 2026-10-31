package service

import (
	"context"
	"errors"
	"strings"
)

func (s *Service) GetNutritionDay(ctx context.Context, owner, date, timezone string) (NutritionDayView, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	if err := validateNutritionDate(date, timezone); err != nil {
		return NutritionDayView{}, err
	}
	record := NutritionDayRecord{Document: NutritionDayDocument{
		Version: 1, Date: date, Timezone: timezone, Entries: []NutritionDiaryEntry{},
	}}
	if err := s.read(ctx, owner, "nutrition/day/"+date, &record); err != nil {
		return NutritionDayView{}, err
	}
	if err := invalidStoredNutrition(record.Document.validate()); err != nil {
		return NutritionDayView{}, err
	}
	if record.Document.Date != date {
		return NutritionDayView{}, invalidStoredNutrition(invalid("stored day does not match its key"))
	}
	totals, err := record.Document.Totals()
	return NutritionDayView{NutritionDayRecord: record, Totals: totals}, err
}

func (s *Service) ReplaceNutritionDay(
	ctx context.Context,
	owner string,
	date string,
	input NutritionDayWrite,
) (NutritionDayView, error) {
	if err := validateNutritionDate(date, "UTC"); err != nil {
		return NutritionDayView{}, err
	}
	initial := NutritionDayRecord{Document: NutritionDayDocument{
		Version: 1, Date: date, Timezone: "UTC", Entries: []NutritionDiaryEntry{},
	}}
	record, err := change(ctx, s, resourceChange[NutritionDayRecord]{
		Owner: owner, Key: "nutrition/day/" + date, RequestID: input.RequestID,
		ExpectedRevision: input.ExpectedRevision, Request: input, Initial: initial,
		Transform: func(value *NutritionDayRecord) error {
			if err := invalidStoredNutrition(value.Document.validate()); err != nil {
				return err
			}
			if value.Document.Date != date {
				return invalidStoredNutrition(invalid("stored day does not match its key"))
			}
			if input.Document.Date != date {
				return invalid("nutrition document date must match the route")
			}
			if err := input.Document.validate(); err != nil {
				return err
			}
			if input.ExpectedRevision > 0 && !sameNutritionGoalSnapshot(value.Document.Goal, input.Document.Goal) {
				return &Error{Code: "nutrition_goal_immutable", Message: "a recorded day keeps its original goal snapshot"}
			}
			if input.ExpectedRevision > 0 && value.Document.Timezone != input.Document.Timezone {
				return &Error{Code: "nutrition_day_immutable", Message: "a recorded day keeps its original timezone"}
			}
			value.Document = input.Document
			return nil
		},
	})
	if err != nil {
		return NutritionDayView{}, err
	}
	totals, err := record.Document.Totals()
	return NutritionDayView{NutritionDayRecord: record, Totals: totals}, err
}

func (s *Service) GetNutritionGoals(ctx context.Context, owner string) (NutritionGoalsRecord, error) {
	record := NutritionGoalsRecord{Document: NutritionGoalsDocument{Version: 1, Goals: []NutritionGoal{}}}
	if err := s.read(ctx, owner, "nutrition/goals", &record); err != nil {
		return NutritionGoalsRecord{}, err
	}
	return record, invalidStoredNutrition(record.Document.validate())
}

func (s *Service) ReplaceNutritionGoals(
	ctx context.Context,
	owner string,
	input NutritionGoalsWrite,
) (NutritionGoalsRecord, error) {
	return change(ctx, s, resourceChange[NutritionGoalsRecord]{
		Owner: owner, Key: "nutrition/goals", RequestID: input.RequestID,
		ExpectedRevision: input.ExpectedRevision, Request: input,
		Initial: NutritionGoalsRecord{Document: NutritionGoalsDocument{Version: 1, Goals: []NutritionGoal{}}},
		Transform: func(value *NutritionGoalsRecord) error {
			if err := invalidStoredNutrition(value.Document.validate()); err != nil {
				return err
			}
			if err := input.Document.validate(); err != nil {
				return err
			}
			value.Document = input.Document
			return nil
		},
	})
}

func (s *Service) GetNutritionRecipes(ctx context.Context, owner string) (NutritionRecipesRecord, error) {
	record := NutritionRecipesRecord{Document: NutritionRecipesDocument{Version: 1, Recipes: []NutritionOwnRecipe{}}}
	if err := s.read(ctx, owner, "nutrition/recipes", &record); err != nil {
		return NutritionRecipesRecord{}, err
	}
	return record, invalidStoredNutrition(record.Document.validate())
}

func (s *Service) ReplaceNutritionRecipes(
	ctx context.Context,
	owner string,
	input NutritionRecipesWrite,
) (NutritionRecipesRecord, error) {
	return change(ctx, s, resourceChange[NutritionRecipesRecord]{
		Owner: owner, Key: "nutrition/recipes", RequestID: input.RequestID,
		ExpectedRevision: input.ExpectedRevision, Request: input,
		Initial: NutritionRecipesRecord{Document: NutritionRecipesDocument{Version: 1, Recipes: []NutritionOwnRecipe{}}},
		Transform: func(value *NutritionRecipesRecord) error {
			if err := invalidStoredNutrition(value.Document.validate()); err != nil {
				return err
			}
			if err := input.Document.validate(); err != nil {
				return err
			}
			value.Document = input.Document
			return nil
		},
	})
}

func (s *Service) GetNutritionPreferences(ctx context.Context, owner string) (NutritionPreferencesRecord, error) {
	record := NutritionPreferencesRecord{Document: NutritionPreferencesDocument{
		Version: 1, FavoriteRecipeIDs: []string{}, FavoriteFoodIDs: []string{},
	}}
	if err := s.read(ctx, owner, "nutrition/preferences", &record); err != nil {
		return NutritionPreferencesRecord{}, err
	}
	return record, invalidStoredNutrition(record.Document.validate())
}

func (s *Service) ReplaceNutritionPreferences(
	ctx context.Context,
	owner string,
	input NutritionPreferencesWrite,
) (NutritionPreferencesRecord, error) {
	return change(ctx, s, resourceChange[NutritionPreferencesRecord]{
		Owner: owner, Key: "nutrition/preferences", RequestID: input.RequestID,
		ExpectedRevision: input.ExpectedRevision, Request: input,
		Initial: NutritionPreferencesRecord{Document: NutritionPreferencesDocument{
			Version: 1, FavoriteRecipeIDs: []string{}, FavoriteFoodIDs: []string{},
		}},
		Transform: func(value *NutritionPreferencesRecord) error {
			if err := invalidStoredNutrition(value.Document.validate()); err != nil {
				return err
			}
			if err := input.Document.validate(); err != nil {
				return err
			}
			value.Document = input.Document
			return nil
		},
	})
}

func invalidStoredNutrition(err error) error {
	if err == nil {
		return nil
	}
	var applicationError *Error
	if errors.As(err, &applicationError) && applicationError.Code == "upgrade_required" {
		return err
	}
	return &Error{Code: "invalid_resource", Message: "stored nutrition data is invalid; original data is preserved"}
}

func sameNutritionGoalSnapshot(first, second *NutritionGoal) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	if first.Target.validate() != nil || second.Target.validate() != nil {
		return false
	}
	sameID := strings.EqualFold(first.ID, second.ID)
	sameDate := first.EffectiveFrom == second.EffectiveFrom
	sameTimezone := first.Timezone == second.Timezone
	if !sameID || !sameDate || !sameTimezone {
		return false
	}
	sameEnergy := *first.Target.EnergyKcal == *second.Target.EnergyKcal
	sameProtein := *first.Target.ProteinGrams == *second.Target.ProteinGrams
	sameFat := *first.Target.FatGrams == *second.Target.FatGrams
	sameCarbohydrate := *first.Target.CarbohydrateGrams == *second.Target.CarbohydrateGrams
	return sameEnergy && sameProtein && sameFat && sameCarbohydrate
}
