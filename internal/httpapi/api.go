package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"podhod/server/internal/service"
)

func New(s *service.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/profile", func(w http.ResponseWriter, r *http.Request) {
		token := sdkauth.TokenInfoFromContext(r.Context())
		if token == nil || token.UserID == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		respond(w, map[string]string{"id": token.UserID, "timezone": "Europe/Moscow"}, nil)
	})
	mux.HandleFunc(
		"GET /v1/template",
		requireScope("schedule:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			result, err := s.GetTemplate(r.Context(), owner)
			if err != nil {
				respond(w, nil, err)
				return
			}
			view, err := s.TemplateView(result)
			respond(w, view, err)
		}),
	)
	mux.HandleFunc(
		"PUT /v1/template",
		requireScope("schedule:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.TemplateWrite
			if !decode(w, r, &input) {
				return
			}
			result, err := s.ReplaceTemplate(r.Context(), owner, input)
			if err != nil {
				respond(w, nil, err)
				return
			}
			view, err := s.TemplateView(result)
			respond(w, view, err)
		}),
	)
	mux.HandleFunc(
		"GET /v1/weeks/{weekStart}",
		requireScope("schedule:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			zone := r.URL.Query().Get("timezone")
			if zone == "" {
				zone = "Europe/Moscow"
			}
			result, err := s.GetWeek(r.Context(), owner, r.PathValue("weekStart"), zone)
			if err != nil {
				respond(w, nil, err)
				return
			}
			view, err := s.WeekView(result)
			respond(w, view, err)
		}),
	)
	mux.HandleFunc(
		"PUT /v1/weeks/{weekStart}",
		requireScope("schedule:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.WeekWrite
			if !decode(w, r, &input) {
				return
			}
			result, err := s.ReplaceWeek(r.Context(), owner, r.PathValue("weekStart"), input)
			if err != nil {
				respond(w, nil, err)
				return
			}
			view, err := s.WeekView(result)
			respond(w, view, err)
		}),
	)
	mux.HandleFunc(
		"POST /v1/weeks/{weekStart}/move",
		requireScope("schedule:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.MoveWorkoutRequest
			if !decode(w, r, &input) {
				return
			}
			result, err := s.MoveWorkout(r.Context(), owner, r.PathValue("weekStart"), input)
			if err != nil {
				respond(w, nil, err)
				return
			}
			view, err := s.WeekView(result)
			respond(w, view, err)
		}),
	)
	mux.HandleFunc(
		"POST /v1/weeks/{weekStart}/rest",
		requireScope("schedule:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.ScheduledRestRequest
			if !decode(w, r, &input) {
				return
			}
			result, err := s.SetScheduledRest(r.Context(), owner, r.PathValue("weekStart"), input)
			if err != nil {
				respond(w, nil, err)
				return
			}
			view, err := s.WeekView(result)
			respond(w, view, err)
		}),
	)
	mux.HandleFunc(
		"GET /v1/active",
		requireScope("workout:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			result, err := s.GetActive(r.Context(), owner)
			respond(w, result, err)
		}),
	)
	mux.HandleFunc(
		"PUT /v1/active",
		requireScope("workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.ActiveWrite
			if !decode(w, r, &input) {
				return
			}
			result, err := s.ReplaceActive(r.Context(), owner, input)
			respond(w, result, err)
		}),
	)
	mux.HandleFunc(
		"POST /v1/active/skip",
		requireScope("workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.SkipExerciseRequest
			if !decode(w, r, &input) {
				return
			}
			result, err := s.SkipExercise(r.Context(), owner, input)
			respond(w, result, err)
		}),
	)
	mux.HandleFunc(
		"POST /v1/active/rest",
		requireScope("workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.ActiveRestRequest
			if !decode(w, r, &input) {
				return
			}
			result, err := s.SetActiveRest(r.Context(), owner, input)
			respond(w, result, err)
		}),
	)
	mux.HandleFunc(
		"GET /v1/history",
		requireScope("workout:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			sessions, err := s.ListHistory(r.Context(), owner)
			respond(w, struct {
				Sessions []service.WorkoutSession `json:"sessions"`
			}{Sessions: sessions}, err)
		}),
	)
	mux.HandleFunc(
		"PUT /v1/history/{sessionID}",
		requireScope("workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.HistoryWrite
			if !decode(w, r, &input) {
				return
			}
			if !strings.EqualFold(input.Session.ID, r.PathValue("sessionID")) {
				respond(w, nil, &service.Error{Code: "invalid_request", Message: "session id does not match route"})
				return
			}
			result, err := s.AppendHistory(r.Context(), owner, input)
			respond(w, result, err)
		}),
	)
	mux.HandleFunc(
		"GET /v1/protein/{date}",
		requireScope("workout:read", func(w http.ResponseWriter, r *http.Request, owner string) {
			result, err := s.GetProtein(r.Context(), owner, r.PathValue("date"))
			respond(w, result, err)
		}),
	)
	mux.HandleFunc(
		"PUT /v1/protein/{date}",
		requireScope("workout:write", func(w http.ResponseWriter, r *http.Request, owner string) {
			var input service.ProteinWrite
			if !decode(w, r, &input) {
				return
			}
			result, err := s.ReplaceProtein(r.Context(), owner, r.PathValue("date"), input)
			respond(w, result, err)
		}),
	)
	mux.HandleFunc("GET /v1/exercises", requireScope("schedule:read", func(w http.ResponseWriter, r *http.Request, _ string) {
		query := r.URL.Query()
		input := service.ExerciseSearch{Query: query.Get("query"), Muscle: query.Get("muscle"), Equipment: query.Get("equipment")}
		for name, destination := range map[string]*int{"limit": &input.Limit, "offset": &input.Offset} {
			if raw := query.Get(name); raw != "" {
				value, err := strconv.Atoi(raw)
				if err != nil || (name == "limit" && value == 0) {
					respond(w, nil, &service.Error{Code: "invalid_request", Message: "invalid pagination parameter"})
					return
				}
				*destination = value
			}
		}
		result, err := s.SearchExercises(input)
		respond(w, result, err)
	}))
	mux.HandleFunc("GET /v1/exercises/{exerciseID}", requireScope("schedule:read", func(w http.ResponseWriter, r *http.Request, _ string) {
		result, err := s.GetExercise(r.PathValue("exerciseID"))
		respond(w, result, err)
	}))
	return mux
}

func requireScope(scope string, next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := sdkauth.TokenInfoFromContext(r.Context())
		if token == nil || token.UserID == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !slices.Contains(token.Scopes, scope) {
			http.Error(w, "insufficient scope", http.StatusForbidden)
			return
		}
		next(w, r, token.UserID)
	}
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		respond(w, nil, &service.Error{Code: "invalid_request", Message: "invalid JSON request"})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond(w, nil, &service.Error{Code: "invalid_request", Message: "request must contain one JSON object"})
		return false
	}
	return true
}

func respond(w http.ResponseWriter, result any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err == nil {
		if err := json.NewEncoder(w).Encode(result); err != nil {
			slog.Warn("response write failed", "error", err)
		}
		return
	}
	var applicationError *service.Error
	if !errors.As(err, &applicationError) {
		slog.Error("training request failed", "error", err)
		applicationError = &service.Error{Code: "internal_error", Message: "server could not complete the request"}
	}
	status := http.StatusBadRequest
	switch applicationError.Code {
	case "revision_conflict", "idempotency_conflict", "history_immutable", "day_occupied", "upgrade_required":
		status = http.StatusConflict
	case "not_found":
		status = http.StatusNotFound
	case "unauthorized":
		status = http.StatusUnauthorized
	case "internal_error":
		status = http.StatusInternalServerError
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(struct {
		Error *service.Error `json:"error"`
	}{Error: applicationError}); err != nil {
		slog.Warn("error response write failed", "error", err)
	}
}
