package service

import "math"

func (f NutritionFacts) AmountForGrams(grams float64) (NutritionTotals, error) {
	if err := f.validatePer100Grams(); err != nil {
		return NutritionTotals{}, err
	}
	if !nutritionMass(grams) {
		return NutritionTotals{}, invalid("food amount requires a positive mass of at most 100000 g")
	}
	return NutritionTotals{
		EnergyKcal: *f.EnergyKcal * grams / 100, ProteinGrams: *f.ProteinGrams * grams / 100,
		FatGrams: *f.FatGrams * grams / 100, CarbohydrateGrams: *f.CarbohydrateGrams * grams / 100,
	}, nil
}

func (n NutritionTotals) add(other NutritionTotals) (NutritionTotals, error) {
	result := NutritionTotals{
		EnergyKcal: n.EnergyKcal + other.EnergyKcal, ProteinGrams: n.ProteinGrams + other.ProteinGrams,
		FatGrams: n.FatGrams + other.FatGrams, CarbohydrateGrams: n.CarbohydrateGrams + other.CarbohydrateGrams,
	}
	if err := result.facts().validate(); err != nil {
		return NutritionTotals{}, err
	}
	return result, nil
}

func (n NutritionTotals) facts() NutritionFacts {
	return NutritionFacts{
		EnergyKcal: &n.EnergyKcal, ProteinGrams: &n.ProteinGrams,
		FatGrams: &n.FatGrams, CarbohydrateGrams: &n.CarbohydrateGrams,
	}
}

func (n NutritionTotals) scale(factor float64) NutritionTotals {
	return NutritionTotals{
		EnergyKcal: n.EnergyKcal * factor, ProteinGrams: n.ProteinGrams * factor,
		FatGrams: n.FatGrams * factor, CarbohydrateGrams: n.CarbohydrateGrams * factor,
	}
}

func (d NutritionDayDocument) Totals() (NutritionTotals, error) {
	var total NutritionTotals
	for _, entry := range d.Entries {
		amount, err := entry.Food.Per100Grams.AmountForGrams(entry.Grams)
		if err != nil {
			return NutritionTotals{}, err
		}
		total, err = total.add(amount)
		if err != nil {
			return NutritionTotals{}, err
		}
	}
	return total, nil
}

func (r NutritionOwnRecipe) Calculate() (NutritionRecipeCalculation, error) {
	finiteServings := !math.IsNaN(r.Servings) && !math.IsInf(r.Servings, 0)
	if !finiteServings || r.Servings <= 0 || r.Servings > 1000 {
		return NutritionRecipeCalculation{}, invalid("invalid recipe servings")
	}
	if !nutritionMass(r.PreparedWeightGrams) || len(r.Ingredients) == 0 {
		return NutritionRecipeCalculation{}, invalid("recipe requires ingredients and a positive ready mass")
	}
	var total NutritionTotals
	for _, ingredient := range r.Ingredients {
		amount, err := ingredient.Food.Per100Grams.AmountForGrams(ingredient.Grams)
		if err != nil {
			return NutritionRecipeCalculation{}, err
		}
		total, err = total.add(amount)
		if err != nil {
			return NutritionRecipeCalculation{}, err
		}
	}
	per100 := total.scale(100 / r.PreparedWeightGrams)
	if err := per100.facts().validatePer100Grams(); err != nil {
		return NutritionRecipeCalculation{}, err
	}
	perServing := total.scale(1 / r.Servings)
	if err := perServing.facts().validate(); err != nil {
		return NutritionRecipeCalculation{}, err
	}
	return NutritionRecipeCalculation{Total: total, PerServing: perServing, Per100Grams: per100}, nil
}
