package service

import (
	_ "embed"
	"encoding/json"
)

//go:embed nutrition-catalog.json
var nutritionCatalogJSON []byte

func BundledNutritionCatalog() json.RawMessage {
	return append(json.RawMessage{}, nutritionCatalogJSON...)
}
