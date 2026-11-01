package service

type NutritionFacts struct {
	EnergyKcal        *float64 `json:"energyKcal"`
	ProteinGrams      *float64 `json:"proteinGrams"`
	FatGrams          *float64 `json:"fatGrams"`
	CarbohydrateGrams *float64 `json:"carbohydrateGrams"`
}

type NutritionTotals struct {
	EnergyKcal        float64 `json:"energyKcal"`
	ProteinGrams      float64 `json:"proteinGrams"`
	FatGrams          float64 `json:"fatGrams"`
	CarbohydrateGrams float64 `json:"carbohydrateGrams"`
}

type NutritionOrigin struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	CatalogVersion string `json:"catalogVersion"`
}

type NutritionFoodSnapshot struct {
	Name        string          `json:"name"`
	Per100Grams NutritionFacts  `json:"per100Grams"`
	Origin      NutritionOrigin `json:"origin"`
}

type NutritionDiaryEntry struct {
	ID    string                `json:"id"`
	Meal  string                `json:"meal"`
	Grams float64               `json:"grams"`
	Food  NutritionFoodSnapshot `json:"food"`
}

type NutritionGoal struct {
	ID            string         `json:"id"`
	EffectiveFrom string         `json:"effectiveFrom"`
	Timezone      string         `json:"timezone"`
	Target        NutritionFacts `json:"target"`
}

type NutritionDayDocument struct {
	Version  int                   `json:"version"`
	Date     string                `json:"date"`
	Timezone string                `json:"timezone"`
	Entries  []NutritionDiaryEntry `json:"entries"`
	Goal     *NutritionGoal        `json:"goal"`
}

type NutritionDayRecord struct {
	Revision int64                `json:"revision"`
	Document NutritionDayDocument `json:"document"`
}

type NutritionDayView struct {
	NutritionDayRecord
	Totals NutritionTotals `json:"totals"`
}

type NutritionDayWrite struct {
	ExpectedRevision int64                `json:"expectedRevision"`
	RequestID        string               `json:"requestID"`
	Document         NutritionDayDocument `json:"document"`
}

type NutritionGoalsDocument struct {
	Version int             `json:"version"`
	Goals   []NutritionGoal `json:"goals"`
}

type NutritionGoalsRecord struct {
	Revision int64                  `json:"revision"`
	Document NutritionGoalsDocument `json:"document"`
}

type NutritionGoalsWrite struct {
	ExpectedRevision int64                  `json:"expectedRevision"`
	RequestID        string                 `json:"requestID"`
	Document         NutritionGoalsDocument `json:"document"`
}

type NutritionRecipeIngredient struct {
	ID    string                `json:"id"`
	Grams float64               `json:"grams"`
	Food  NutritionFoodSnapshot `json:"food"`
}

type NutritionOwnRecipe struct {
	ID                  string                      `json:"id"`
	Name                string                      `json:"name"`
	Category            string                      `json:"category"`
	Minutes             int                         `json:"minutes"`
	Servings            float64                     `json:"servings"`
	PreparedWeightGrams float64                     `json:"preparedWeightGrams"`
	Ingredients         []NutritionRecipeIngredient `json:"ingredients"`
	Steps               []string                    `json:"steps"`
}

type NutritionRecipeCalculation struct {
	Total       NutritionTotals `json:"total"`
	PerServing  NutritionTotals `json:"perServing"`
	Per100Grams NutritionTotals `json:"per100Grams"`
}

type NutritionRecipesDocument struct {
	Version int                  `json:"version"`
	Recipes []NutritionOwnRecipe `json:"recipes"`
}

type NutritionRecipesRecord struct {
	Revision int64                    `json:"revision"`
	Document NutritionRecipesDocument `json:"document"`
}

type NutritionRecipesWrite struct {
	ExpectedRevision int64                    `json:"expectedRevision"`
	RequestID        string                   `json:"requestID"`
	Document         NutritionRecipesDocument `json:"document"`
}

type CreateNutritionRecipeRequest struct {
	ExpectedRevision int64              `json:"expectedRevision"`
	RequestID        string             `json:"requestID"`
	Recipe           NutritionOwnRecipe `json:"recipe"`
}

type DeleteNutritionRecipeRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	RequestID        string `json:"requestID"`
	RecipeID         string `json:"recipeID"`
}

type UpdateNutritionRecipeRequest struct {
	ExpectedRevision int64              `json:"expectedRevision"`
	RequestID        string             `json:"requestID"`
	RecipeID         string             `json:"recipeID"`
	Recipe           NutritionOwnRecipe `json:"recipe"`
}

type LogNutritionEntryRequest struct {
	ExpectedRevision int64               `json:"expectedRevision"`
	RequestID        string              `json:"requestID"`
	Timezone         string              `json:"timezone"`
	InitialGoal      *NutritionGoal      `json:"initialGoal"`
	Entry            NutritionDiaryEntry `json:"entry"`
}

type NutritionPreferencesDocument struct {
	Version           int      `json:"version"`
	FavoriteRecipeIDs []string `json:"favoriteRecipeIDs"`
	FavoriteFoodIDs   []string `json:"favoriteFoodIDs"`
}

type NutritionPreferencesRecord struct {
	Revision int64                        `json:"revision"`
	Document NutritionPreferencesDocument `json:"document"`
}

type NutritionPreferencesWrite struct {
	ExpectedRevision int64                        `json:"expectedRevision"`
	RequestID        string                       `json:"requestID"`
	Document         NutritionPreferencesDocument `json:"document"`
}
