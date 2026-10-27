//go:build integration

package storage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"podhod/server/internal/httpapi"
	"podhod/server/internal/service"
)

func TestUsersHaveIndependentResourcesRevisionsAndIdempotency(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	for _, owner := range []string{"user-a", "user-b"} {
		days := make([]service.TrainingDay, 7)
		for i := range days {
			days[i] = service.TrainingDay{Weekday: i + 1, Title: owner, Exercises: []service.PlannedExercise{}}
		}
		if _, err := s.ReplaceTemplate(t.Context(), owner, service.TemplateWrite{
			RequestID: requestID(50), Days: days,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ReplaceActive(t.Context(), owner, service.ActiveWrite{
			RequestID: requestID(51), State: &service.SessionState{Session: activeSession()},
		}); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if _, err := s.ReplaceProtein(t.Context(), owner, "2026-10-27", service.ProteinWrite{
			RequestID: requestID(52), MorningTakenAt: &now,
		}); err != nil {
			t.Fatal(err)
		}
		session := activeSession()
		session.Title = owner
		finish := session.StartedAt.Add(time.Minute)
		session.FinishedAt = &finish
		if _, err := s.AppendHistory(t.Context(), owner, service.HistoryWrite{
			RequestID: requestID(53), Session: session,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"user-a", "user-b"} {
		template, err := s.GetTemplate(t.Context(), owner)
		if err != nil || template.Revision != 1 || template.Days[0].Title != owner {
			t.Fatalf("template isolation failed for %s: %+v %v", owner, template, err)
		}
		history, err := s.ListHistory(t.Context(), owner)
		if err != nil || len(history) != 1 || history[0].Title != owner {
			t.Fatalf("history isolation failed for %s: %+v %v", owner, history, err)
		}
	}
	active, err := s.GetActive(t.Context(), "new-user")
	if err != nil || active.State != nil || active.Revision != 0 {
		t.Fatalf("new user received another user's active workout: %+v %v", active, err)
	}
	protein, err := s.GetProtein(t.Context(), "new-user", "2026-10-27")
	if err != nil || protein.Revision != 0 || protein.MorningTakenAt != nil {
		t.Fatalf("new user received another user's protein record: %+v %v", protein, err)
	}
}

func TestHTTPResourceOwnerAlwaysComesFromVerifiedSubject(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	days := make([]service.TrainingDay, 7)
	for i := range days {
		days[i] = service.TrainingDay{Weekday: i + 1, Title: "private-a", Exercises: []service.PlannedExercise{}}
	}
	if _, err := s.ReplaceTemplate(t.Context(), "user-a", service.TemplateWrite{
		RequestID: requestID(60), Days: days,
	}); err != nil {
		t.Fatal(err)
	}
	verifier := func(_ context.Context, raw string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		return &sdkauth.TokenInfo{
			UserID: raw, Scopes: []string{"schedule:read", "workout:read"}, Expiration: time.Now().Add(time.Minute),
		}, nil
	}
	handler := sdkauth.RequireBearerToken(verifier, nil)(httpapi.New(s))
	request := httptest.NewRequest(http.MethodGet, "/v1/template?owner=user-a&userID=user-a", nil)
	request.Header.Set("Authorization", "Bearer user-b")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP %d", response.Code)
	}
	var template service.Template
	if err := json.Unmarshal(response.Body.Bytes(), &template); err != nil {
		t.Fatal(err)
	}
	if template.Revision != 0 || template.Days[0].Title == "private-a" {
		t.Fatal("query parameters selected another user's resources")
	}
}
