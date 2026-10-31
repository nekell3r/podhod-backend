package service

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (f NutritionFacts) validate() error {
	values := []*float64{f.EnergyKcal, f.ProteinGrams, f.FatGrams, f.CarbohydrateGrams}
	for _, value := range values {
		if value == nil {
			return invalid("all four nutrition values must be explicitly provided")
		}
		finite := !math.IsNaN(*value) && !math.IsInf(*value, 0)
		if !finite || *value < 0 {
			return invalid("nutrition values must be finite and nonnegative")
		}
	}
	return nil
}

func (f NutritionFacts) validatePer100Grams() error {
	if err := f.validate(); err != nil {
		return err
	}
	macrosBounded := *f.ProteinGrams <= 100 && *f.FatGrams <= 100 && *f.CarbohydrateGrams <= 100
	massBounded := *f.ProteinGrams+*f.FatGrams+*f.CarbohydrateGrams <= 101
	if *f.EnergyKcal > 1000 || !macrosBounded || !massBounded {
		return invalid("nutrition per 100 g exceeds supported bounds")
	}
	return nil
}

func (o NutritionOrigin) validate() error {
	switch o.Kind {
	case "manual":
		if o.ID != "" || o.CatalogVersion != "" {
			return invalid("manual food must not claim a catalog or recipe identity")
		}
	case "catalogFood", "catalogRecipe":
		if !nutritionText(o.ID, 200) || !nutritionText(o.CatalogVersion, 100) {
			return invalid("catalog origin requires an id and version")
		}
		if o.Kind == "catalogFood" {
			id, err := strconv.Atoi(strings.TrimPrefix(o.ID, "usda-"))
			canonical := o.ID == "usda-"+strconv.Itoa(id)
			if !canonical || err != nil || id <= 0 {
				return invalid("catalog food requires an exact USDA id")
			}
		}
	case "ownRecipe":
		if !validUUID(o.ID) || o.CatalogVersion != "" {
			return invalid("own recipe origin requires a UUID without a catalog version")
		}
	default:
		return invalid("unknown nutrition origin")
	}
	return nil
}

func (f NutritionFoodSnapshot) validate() error {
	if !nutritionText(f.Name, 200) {
		return invalid("food name must contain 1 to 200 characters")
	}
	if err := f.Per100Grams.validatePer100Grams(); err != nil {
		return err
	}
	return f.Origin.validate()
}

func (g NutritionGoal) validate() error {
	if !validUUID(g.ID) {
		return invalid("goal id must be a UUID")
	}
	if err := validateNutritionDate(g.EffectiveFrom, g.Timezone); err != nil {
		return err
	}
	if err := g.Target.validate(); err != nil {
		return err
	}
	energyBounded := *g.Target.EnergyKcal > 0 && *g.Target.EnergyKcal <= 20_000
	macrosBounded := *g.Target.ProteinGrams <= 3000 && *g.Target.FatGrams <= 3000 && *g.Target.CarbohydrateGrams <= 3000
	if !energyBounded || !macrosBounded {
		return invalid("manual nutrition target exceeds supported bounds")
	}
	return nil
}

func (d NutritionDayDocument) validate() error {
	if err := validateNutritionVersion(d.Version); err != nil {
		return err
	}
	if err := validateNutritionDate(d.Date, d.Timezone); err != nil {
		return err
	}
	if d.Entries == nil || len(d.Entries) > 100 {
		return invalid("entries must be an explicit array of at most 100 items")
	}
	seen := map[string]bool{}
	for _, entry := range d.Entries {
		id := strings.ToLower(entry.ID)
		if !validUUID(id) || seen[id] {
			return invalid("entry ids must be unique UUIDs")
		}
		seen[id] = true
		switch entry.Meal {
		case "breakfast", "lunch", "dinner", "snack":
		default:
			return invalid("unknown meal type")
		}
		if !nutritionMass(entry.Grams) {
			return invalid("entry mass must be greater than zero and at most 100000 g")
		}
		if err := entry.Food.validate(); err != nil {
			return err
		}
	}
	if d.Goal != nil {
		if err := d.Goal.validate(); err != nil {
			return err
		}
		if d.Goal.EffectiveFrom > d.Date {
			return invalid("day goal must not begin after the recorded day")
		}
	}
	return nil
}

func (d NutritionGoalsDocument) validate() error {
	if err := validateNutritionVersion(d.Version); err != nil {
		return err
	}
	if d.Goals == nil || len(d.Goals) > 100 {
		return invalid("goals must be an explicit array of at most 100 items")
	}
	ids := map[string]bool{}
	dates := map[string]bool{}
	for _, goal := range d.Goals {
		id := strings.ToLower(goal.ID)
		if ids[id] || dates[goal.EffectiveFrom] {
			return invalid("goal ids and effective dates must be unique")
		}
		ids[id] = true
		dates[goal.EffectiveFrom] = true
		if err := goal.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r NutritionOwnRecipe) validate() error {
	if !validUUID(r.ID) || !nutritionText(r.Name, 200) {
		return invalid("recipe requires a UUID and a name of 1 to 200 characters")
	}
	switch r.Category {
	case "breakfast", "main", "salad", "snack":
	default:
		return invalid("unknown recipe category")
	}
	if r.Minutes < 1 || r.Minutes > 1440 {
		return invalid("recipe time must be between 1 and 1440 minutes")
	}
	finiteServings := !math.IsNaN(r.Servings) && !math.IsInf(r.Servings, 0)
	if !finiteServings || r.Servings <= 0 || r.Servings > 1000 {
		return invalid("recipe servings must be greater than zero and at most 1000")
	}
	if !nutritionMass(r.PreparedWeightGrams) {
		return invalid("recipe requires a positive ready weight of at most 100000 g")
	}
	if len(r.Ingredients) < 1 || len(r.Ingredients) > 50 {
		return invalid("recipe requires 1 to 50 ingredients")
	}
	seen := map[string]bool{}
	for _, ingredient := range r.Ingredients {
		id := strings.ToLower(ingredient.ID)
		if !validUUID(id) || seen[id] {
			return invalid("ingredient ids must be unique UUIDs")
		}
		seen[id] = true
		if !nutritionMass(ingredient.Grams) {
			return invalid("ingredient requires a positive mass of at most 100000 g")
		}
		if err := ingredient.Food.validate(); err != nil {
			return err
		}
	}
	if len(r.Steps) < 1 || len(r.Steps) > 30 {
		return invalid("recipe requires 1 to 30 preparation steps")
	}
	for _, step := range r.Steps {
		if !nutritionText(step, 2000) {
			return invalid("each recipe step must contain 1 to 2000 characters")
		}
	}
	_, err := r.Calculate()
	return err
}

func (d NutritionRecipesDocument) validate() error {
	if err := validateNutritionVersion(d.Version); err != nil {
		return err
	}
	if d.Recipes == nil || len(d.Recipes) > 100 {
		return invalid("recipes must be an explicit array of at most 100 items")
	}
	seen := map[string]bool{}
	for _, recipe := range d.Recipes {
		id := strings.ToLower(recipe.ID)
		if seen[id] {
			return invalid("recipe ids must be unique")
		}
		seen[id] = true
		if err := recipe.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (d NutritionPreferencesDocument) validate() error {
	if err := validateNutritionVersion(d.Version); err != nil {
		return err
	}
	if d.FavoriteRecipeIDs == nil || d.FavoriteFoodIDs == nil {
		return invalid("favorite lists must be explicit arrays")
	}
	if len(d.FavoriteRecipeIDs)+len(d.FavoriteFoodIDs) > 500 {
		return invalid("at most 500 favorites are supported")
	}
	for _, ids := range [][]string{d.FavoriteRecipeIDs, d.FavoriteFoodIDs} {
		seen := map[string]bool{}
		for _, id := range ids {
			if !nutritionText(id, 200) || strings.TrimSpace(id) != id || seen[id] {
				return invalid("favorite ids must be unique nonempty identifiers without surrounding whitespace")
			}
			seen[id] = true
		}
	}
	return nil
}

func validateNutritionVersion(version int) error {
	if version > 1 {
		return &Error{Code: "upgrade_required", Message: "update the app to preserve the nutrition document format"}
	}
	if version != 1 {
		return invalid("nutrition document version must be 1")
	}
	return nil
}

func validateNutritionDate(date, timezone string) error {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || len(date) != 10 {
		return invalid("nutrition date must be YYYY-MM-DD")
	}
	if parsed.Year() < 1900 || parsed.Year() > 9999 {
		return invalid("nutrition date must be between 1900 and 9999")
	}
	if timezone == "" || timezone == "Local" {
		return invalid("nutrition timezone must be an explicit IANA timezone")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return invalid("unknown nutrition timezone")
	}
	return nil
}

func nutritionText(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= maximum
}

func nutritionMass(grams float64) bool {
	finite := !math.IsNaN(grams) && !math.IsInf(grams, 0)
	return finite && grams > 0 && grams <= 100_000
}
