package service

import (
	"context"
	"strings"
)

func (s *Service) CreateNutritionRecipe(
	ctx context.Context,
	owner string,
	input CreateNutritionRecipeRequest,
) (NutritionRecipesRecord, error) {
	return change(ctx, s, resourceChange[NutritionRecipesRecord]{
		Owner: owner, Key: "nutrition/recipes", RequestID: input.RequestID,
		ExpectedRevision: input.ExpectedRevision, Request: input,
		Transform: func(value *NutritionRecipesRecord) error {
			if input.ExpectedRevision == 0 {
				value.Document = NutritionRecipesDocument{Version: 1, Recipes: []NutritionOwnRecipe{}}
			}
			if err := invalidStoredNutrition(value.Document.validate()); err != nil {
				return err
			}
			for _, recipe := range value.Document.Recipes {
				if strings.EqualFold(recipe.ID, input.Recipe.ID) {
					return &Error{Code: "recipe_exists", Message: "a recipe with this UUID already exists"}
				}
			}
			value.Document.Recipes = append(value.Document.Recipes, input.Recipe)
			return value.Document.validate()
		},
	})
}

func (s *Service) UpdateNutritionRecipe(
	ctx context.Context,
	owner string,
	input UpdateNutritionRecipeRequest,
) (NutritionRecipesRecord, error) {
	return change(ctx, s, resourceChange[NutritionRecipesRecord]{
		Owner: owner, Key: "nutrition/recipes", RequestID: input.RequestID,
		ExpectedRevision: input.ExpectedRevision, Request: input,
		Transform: func(value *NutritionRecipesRecord) error {
			if input.ExpectedRevision == 0 {
				value.Document = NutritionRecipesDocument{Version: 1, Recipes: []NutritionOwnRecipe{}}
			}
			if err := invalidStoredNutrition(value.Document.validate()); err != nil {
				return err
			}
			if !validUUID(input.RecipeID) || !strings.EqualFold(input.RecipeID, input.Recipe.ID) {
				return invalid("recipe id must be a UUID matching the unchanged recipe UUID")
			}
			if err := input.Recipe.validate(); err != nil {
				return err
			}
			for i, recipe := range value.Document.Recipes {
				if strings.EqualFold(recipe.ID, input.RecipeID) {
					replacement := input.Recipe
					replacement.ID = recipe.ID
					value.Document.Recipes[i] = replacement
					return value.Document.validate()
				}
			}
			return &Error{Code: "not_found", Message: "own recipe not found"}
		},
	})
}

func (s *Service) DeleteNutritionRecipe(
	ctx context.Context,
	owner string,
	input DeleteNutritionRecipeRequest,
) (NutritionRecipesRecord, error) {
	return change(ctx, s, resourceChange[NutritionRecipesRecord]{
		Owner: owner, Key: "nutrition/recipes", RequestID: input.RequestID,
		ExpectedRevision: input.ExpectedRevision, Request: input,
		Transform: func(value *NutritionRecipesRecord) error {
			if input.ExpectedRevision == 0 {
				value.Document = NutritionRecipesDocument{Version: 1, Recipes: []NutritionOwnRecipe{}}
			}
			if err := invalidStoredNutrition(value.Document.validate()); err != nil {
				return err
			}
			if !validUUID(input.RecipeID) {
				return invalid("recipe id must be a UUID")
			}
			for i, recipe := range value.Document.Recipes {
				if strings.EqualFold(recipe.ID, input.RecipeID) {
					value.Document.Recipes = append(value.Document.Recipes[:i], value.Document.Recipes[i+1:]...)
					return nil
				}
			}
			return &Error{Code: "not_found", Message: "own recipe not found"}
		},
	})
}

func (s *Service) LogNutritionEntry(
	ctx context.Context,
	owner, date string,
	input LogNutritionEntryRequest,
) (NutritionDayView, error) {
	if err := validateNutritionDate(date, input.Timezone); err != nil {
		return NutritionDayView{}, err
	}
	record, err := change(ctx, s, resourceChange[NutritionDayRecord]{
		Owner: owner, Key: "nutrition/day/" + date, RequestID: input.RequestID,
		ExpectedRevision: input.ExpectedRevision, Request: input,
		Transform: func(value *NutritionDayRecord) error {
			if input.ExpectedRevision == 0 {
				value.Document = NutritionDayDocument{
					Version: 1, Date: date, Timezone: input.Timezone, Entries: []NutritionDiaryEntry{},
				}
			}
			if err := invalidStoredNutrition(value.Document.validate()); err != nil {
				return err
			}
			if value.Document.Date != date {
				return invalidStoredNutrition(invalid("stored day does not match its key"))
			}
			if value.Document.Timezone != input.Timezone {
				return &Error{Code: "nutrition_day_immutable", Message: "a recorded day keeps its original timezone"}
			}
			if input.ExpectedRevision == 0 {
				value.Document.Goal = input.InitialGoal
			}
			value.Document.Entries = append(value.Document.Entries, input.Entry)
			return value.Document.validate()
		},
	})
	if err != nil {
		return NutritionDayView{}, err
	}
	totals, err := record.Document.Totals()
	return NutritionDayView{NutritionDayRecord: record, Totals: totals}, err
}
