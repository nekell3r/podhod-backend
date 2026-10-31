package service

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func nutritionFoodFixture() NutritionFoodSnapshot {
	return NutritionFoodSnapshot{
		Name: "Test food", Origin: NutritionOrigin{Kind: "manual"},
		Per100Grams: NutritionTotals{EnergyKcal: 150, ProteinGrams: 10, FatGrams: 10, CarbohydrateGrams: 5}.facts(),
	}
}

func nutritionDayFixture() NutritionDayDocument {
	return NutritionDayDocument{Version: 1, Date: "2026-10-31", Timezone: "Europe/Moscow",
		Entries: []NutritionDiaryEntry{{
			ID: "00000000-0000-4000-8000-000000000001", Meal: "breakfast", Grams: 150, Food: nutritionFoodFixture(),
		}},
	}
}

func nutritionRecipeFixture() NutritionOwnRecipe {
	return NutritionOwnRecipe{
		ID: "00000000-0000-4000-8000-000000000002", Name: "Test recipe", Category: "breakfast",
		Minutes: 10, Servings: 2, PreparedWeightGrams: 300,
		Ingredients: []NutritionRecipeIngredient{{
			ID: "00000000-0000-4000-8000-000000000003", Grams: 200, Food: nutritionFoodFixture(),
		}},
		Steps: []string{"Prepare the ingredients", "Weigh the ready dish"},
	}
}

func TestNutritionFactsRequireAllValuesAndPreserveSourceEnergy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		body  string
		valid bool
	}{
		{name: "explicit water zeros",
			body: `{"energyKcal":0,"proteinGrams":0,"fatGrams":0,"carbohydrateGrams":0}`, valid: true},
		{name: "missing is unknown", body: `{"energyKcal":884,"fatGrams":100,"carbohydrateGrams":0}`},
		{name: "null is unknown", body: `{"energyKcal":884,"proteinGrams":null,"fatGrams":100,"carbohydrateGrams":0}`},
		{name: "source olive oil",
			body: `{"energyKcal":884,"proteinGrams":0,"fatGrams":100,"carbohydrateGrams":0}`, valid: true},
		{name: "negative source", body: `{"energyKcal":100,"proteinGrams":1,"fatGrams":1,"carbohydrateGrams":-0.1}`},
		{name: "mass impossible", body: `{"energyKcal":100,"proteinGrams":90,"fatGrams":20,"carbohydrateGrams":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var facts NutritionFacts
			if err := json.Unmarshal([]byte(tc.body), &facts); err != nil {
				t.Fatal(err)
			}
			if err := facts.validatePer100Grams(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if tc.name == "source olive oil" {
				amount, err := facts.AmountForGrams(10)
				if err != nil || math.Abs(amount.EnergyKcal-88.4) > 1e-9 || amount.FatGrams != 10 {
					t.Fatalf("source energy changed: %+v %v", amount, err)
				}
			}
		})
	}
}

func TestNutritionRejectsInvalidNumbersAndCalendarBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*NutritionDayDocument)
	}{
		{name: "missing entries", mutate: func(d *NutritionDayDocument) { d.Entries = nil }},
		{name: "duplicate entry", mutate: func(d *NutritionDayDocument) { d.Entries = append(d.Entries, d.Entries[0]) }},
		{name: "future format", mutate: func(d *NutritionDayDocument) { d.Version = 2 }},
		{name: "missing version", mutate: func(d *NutritionDayDocument) { d.Version = 0 }},
		{name: "impossible date", mutate: func(d *NutritionDayDocument) { d.Date = "2026-02-30" }},
		{name: "old date", mutate: func(d *NutritionDayDocument) { d.Date = "1899-12-31" }},
		{name: "missing timezone", mutate: func(d *NutritionDayDocument) { d.Timezone = "" }},
		{name: "host timezone", mutate: func(d *NutritionDayDocument) { d.Timezone = "Local" }},
		{name: "unknown timezone", mutate: func(d *NutritionDayDocument) { d.Timezone = "Not/AZone" }},
		{name: "invalid meal", mutate: func(d *NutritionDayDocument) { d.Entries[0].Meal = "medicine" }},
		{name: "zero grams", mutate: func(d *NutritionDayDocument) { d.Entries[0].Grams = 0 }},
		{name: "huge grams", mutate: func(d *NutritionDayDocument) { d.Entries[0].Grams = 100001 }},
		{name: "infinite grams", mutate: func(d *NutritionDayDocument) { d.Entries[0].Grams = math.Inf(1) }},
		{name: "nan grams", mutate: func(d *NutritionDayDocument) { d.Entries[0].Grams = math.NaN() }},
		{name: "nan facts", mutate: func(d *NutritionDayDocument) { *d.Entries[0].Food.Per100Grams.EnergyKcal = math.NaN() }},
		{name: "infinite facts",
			mutate: func(d *NutritionDayDocument) { *d.Entries[0].Food.Per100Grams.EnergyKcal = math.Inf(1) }},
		{name: "empty name", mutate: func(d *NutritionDayDocument) { d.Entries[0].Food.Name = "  " }},
		{name: "manual catalog claim", mutate: func(d *NutritionDayDocument) { d.Entries[0].Food.Origin.ID = "usda-17" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			document := nutritionDayFixture()
			tc.mutate(&document)
			if err := document.validate(); err == nil {
				t.Fatal("invalid day was accepted")
			}
		})
	}
	day := nutritionDayFixture()
	day.Date = "2026-11-01"
	day.Timezone = "America/New_York"
	if err := day.validate(); err != nil {
		t.Fatalf("DST date rejected: %v", err)
	}
}

func TestNutritionRecipeReadyMassAndFractionalServings(t *testing.T) {
	t.Parallel()
	recipe := nutritionRecipeFixture()
	calculation, err := recipe.Calculate()
	if err != nil {
		t.Fatal(err)
	}
	if calculation.Total.EnergyKcal != 300 || calculation.PerServing.EnergyKcal != 150 {
		t.Fatalf("unexpected recipe totals: %+v", calculation)
	}
	recipe.PreparedWeightGrams = 200
	recipe.Servings = 1.5
	drier, err := recipe.Calculate()
	if err != nil {
		t.Fatal(err)
	}
	if drier.Total != calculation.Total || drier.Per100Grams.EnergyKcal != 150 || drier.PerServing.EnergyKcal != 200 {
		t.Fatalf("ready mass invented nutrition or fractional servings failed: %+v", drier)
	}
	recipe.PreparedWeightGrams = 1
	if _, err := recipe.Calculate(); err == nil {
		t.Fatal("ready mass below the nutrient mass was accepted")
	}
	recipe.PreparedWeightGrams = 200
	recipe.Servings = math.SmallestNonzeroFloat64
	if _, err := recipe.Calculate(); err == nil {
		t.Fatal("overflowed per-serving calculation was accepted")
	}
}

func TestNutritionTargetsAndPreferencesHaveExplicitVersionedBounds(t *testing.T) {
	t.Parallel()
	goal := NutritionGoal{
		ID: "00000000-0000-4000-8000-000000000004", EffectiveFrom: "2026-10-31", Timezone: "Europe/Moscow",
		Target: NutritionTotals{EnergyKcal: 2200, ProteinGrams: 130, FatGrams: 70, CarbohydrateGrams: 250}.facts(),
	}
	if err := (NutritionGoalsDocument{Version: 1, Goals: []NutritionGoal{goal}}).validate(); err != nil {
		t.Fatal(err)
	}
	day := nutritionDayFixture()
	day.Goal = &goal
	goal.EffectiveFrom = "2026-11-01"
	if err := day.validate(); err == nil {
		t.Fatal("future goal applied to an earlier day")
	}
	preferences := NutritionPreferencesDocument{
		Version: 1, FavoriteRecipeIDs: []string{"unknown-future-recipe"}, FavoriteFoodIDs: []string{},
	}
	if err := preferences.validate(); err != nil {
		t.Fatalf("unknown favorite was lost: %v", err)
	}
	preferences.FavoriteFoodIDs = nil
	if err := preferences.validate(); err == nil {
		t.Fatal("missing list silently cleared favorites")
	}
	preferences.FavoriteFoodIDs = []string{"usda-17", "usda-17"}
	if err := preferences.validate(); err == nil {
		t.Fatal("duplicate favorites accepted")
	}
	preferences.FavoriteFoodIDs = []string{}
	for range 500 {
		preferences.FavoriteFoodIDs = append(preferences.FavoriteFoodIDs, "another")
	}
	if err := preferences.validate(); err == nil {
		t.Fatal("combined favorites bound ignored")
	}
}

func TestNutritionRecipeValidationAndServerTotalsDoNotMutateSnapshots(t *testing.T) {
	t.Parallel()
	day := nutritionDayFixture()
	before, err := json.Marshal(day)
	if err != nil {
		t.Fatal(err)
	}
	totals, err := day.Totals()
	if err != nil || totals.EnergyKcal != 225 || totals.ProteinGrams != 15 {
		t.Fatalf("unexpected day total: %+v %v", totals, err)
	}
	after, err := json.Marshal(day)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("calculation mutated original snapshot")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*NutritionOwnRecipe)
	}{
		{name: "empty ingredients", mutate: func(r *NutritionOwnRecipe) { r.Ingredients = nil }},
		{name: "duplicate ingredient id",
			mutate: func(r *NutritionOwnRecipe) { r.Ingredients = append(r.Ingredients, r.Ingredients[0]) }},
		{name: "no steps", mutate: func(r *NutritionOwnRecipe) { r.Steps = nil }},
		{name: "missing ready mass", mutate: func(r *NutritionOwnRecipe) { r.PreparedWeightGrams = 0 }},
		{name: "invalid category", mutate: func(r *NutritionOwnRecipe) { r.Category = "automatic-diet" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := nutritionRecipeFixture()
			tc.mutate(&r)
			if err := r.validate(); err == nil {
				t.Fatal("invalid own recipe accepted")
			}
		})
	}
}

func TestNutritionPublicCatalogIsValidAndIndependentOfCallerMutation(t *testing.T) {
	t.Parallel()
	first := BundledNutritionCatalog()
	first[0] = '!'
	var catalog struct {
		Foods   []json.RawMessage `json:"foods"`
		Recipes []json.RawMessage `json:"recipes"`
	}
	if err := json.Unmarshal(BundledNutritionCatalog(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Foods) != 24 || len(catalog.Recipes) != 16 {
		t.Fatalf("unexpected catalog: foods=%d recipes=%d", len(catalog.Foods), len(catalog.Recipes))
	}
}

func TestNutritionCatalogOriginsUseExactIdentifiers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		id    string
		valid bool
	}{
		{name: "canonical", id: "usda-171413", valid: true},
		{name: "leading zero", id: "usda-0171413"},
		{name: "plus sign", id: "usda-+171413"},
		{name: "wrong prefix", id: "other-171413"},
		{name: "no numeric id", id: "usda-oil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (NutritionOrigin{Kind: "catalogFood", ID: tc.id, CatalogVersion: "2026.10.31.2"}).validate()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}
