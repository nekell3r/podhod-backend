package httpapi

import (
	"net/http"

	"podhod/server/internal/service"
)

func PublicNutritionCatalog(w http.ResponseWriter, r *http.Request) {
	respond(w, service.BundledNutritionCatalog(), nil)
}

func nutritionRoutes(mux *http.ServeMux, s *service.Service) {
	mux.HandleFunc("GET /v1/nutrition/catalog", PublicNutritionCatalog)
	mux.HandleFunc("GET /v1/nutrition/days/{date}", requireScope(
		"workout:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			result, err := s.GetNutritionDay(
				r.Context(),
				owner,
				r.PathValue("date"),
				r.URL.Query().Get("timezone"),
			)
			respond(w, result, err)
		},
	))
	mux.HandleFunc("PUT /v1/nutrition/days/{date}", requireScope(
		"workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.NutritionDayWrite
			if !decode(w, r, &input) {
				return
			}
			result, err := s.ReplaceNutritionDay(
				r.Context(),
				owner,
				r.PathValue("date"),
				input,
			)
			respond(w, result, err)
		},
	))
	mux.HandleFunc("GET /v1/nutrition/goals", requireScope(
		"workout:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			result, err := s.GetNutritionGoals(r.Context(), owner)
			respond(w, result, err)
		},
	))
	mux.HandleFunc("PUT /v1/nutrition/goals", requireScope(
		"workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.NutritionGoalsWrite
			if !decode(w, r, &input) {
				return
			}
			result, err := s.ReplaceNutritionGoals(r.Context(), owner, input)
			respond(w, result, err)
		},
	))
	mux.HandleFunc("GET /v1/nutrition/recipes", requireScope(
		"workout:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			result, err := s.GetNutritionRecipes(r.Context(), owner)
			respond(w, result, err)
		},
	))
	mux.HandleFunc("PUT /v1/nutrition/recipes", requireScope(
		"workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.NutritionRecipesWrite
			if !decode(w, r, &input) {
				return
			}
			result, err := s.ReplaceNutritionRecipes(r.Context(), owner, input)
			respond(w, result, err)
		},
	))
	mux.HandleFunc("GET /v1/nutrition/preferences", requireScope(
		"workout:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			result, err := s.GetNutritionPreferences(r.Context(), owner)
			respond(w, result, err)
		},
	))
	mux.HandleFunc("PUT /v1/nutrition/preferences", requireScope(
		"workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.NutritionPreferencesWrite
			if !decode(w, r, &input) {
				return
			}
			result, err := s.ReplaceNutritionPreferences(r.Context(), owner, input)
			respond(w, result, err)
		},
	))
}
