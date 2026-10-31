package httpapi

import (
	"net/http"

	"podhod/server/internal/service"
)

func cycleRoutes(mux *http.ServeMux, s *service.Service) {
	mux.HandleFunc("GET /v1/cycle", requireScope("schedule:read", func(w http.ResponseWriter, r *http.Request, owner string) {
		result, err := s.GetTrainingCycle(r.Context(), owner)
		respond(w, result, err)
	}))
	mux.HandleFunc("PUT /v1/cycle", requireScope("schedule:write", func(w http.ResponseWriter, r *http.Request, owner string) {
		var input service.TrainingCycleWrite
		if !decode(w, r, &input) {
			return
		}
		result, err := s.ReplaceTrainingCycle(r.Context(), owner, input)
		respond(w, result, err)
	}))
}
