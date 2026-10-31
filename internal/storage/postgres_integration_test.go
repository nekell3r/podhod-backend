//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"podhod/server/internal/httpapi"
	"podhod/server/internal/service"
	"podhod/server/internal/storage"
)

func databaseService(t *testing.T) *service.Service {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL must point to an isolated test database")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	schema := "test_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	repo := storage.New(pool)
	if err := repo.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return service.New(repo)
}

func requestID(index byte) string {
	id := make([]byte, 16)
	id[15] = index
	h := hex.EncodeToString(id)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func plan() service.PlannedExercise {
	return service.PlannedExercise{ID: requestID(10), Exercise: service.Exercise{ID: requestID(11), Name: "Жим"},
		SetCount: 2, TargetReps: "8–12", RestSeconds: 90, RestAfterExerciseSeconds: 180}
}

func TestConcurrentCASAndOwnerIsolation(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ReplaceWeek(t.Context(), "owner-a", "2026-10-26", service.WeekWrite{
				RequestID: requestID(byte(20 + i)), Timezone: "Europe/Moscow", Workouts: []service.ScheduledWorkout{},
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var conflict *service.Error
		if !errors.As(err, &conflict) || conflict.Code != "revision_conflict" || conflict.CurrentRevision != 1 {
			t.Fatalf("unexpected result: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent writes=%d, want1", successes)
	}
	other, err := s.GetWeek(t.Context(), "owner-b", "2026-10-26", "Europe/Moscow")
	if err != nil || other.Revision != 0 || len(other.Workouts) != 0 {
		t.Fatalf("other owner state leaked: %+v %v", other, err)
	}
}

func TestIdempotentReplayAndOccupiedMove(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	input := service.WeekWrite{RequestID: requestID(1), Timezone: "Europe/Moscow", Workouts: []service.ScheduledWorkout{
		{ID: requestID(2), PlannedDate: "2026-10-26", Title: "Верх", Exercises: []service.PlannedExercise{plan()}},
		{ID: requestID(3), PlannedDate: "2026-10-28", Title: "Ноги", Exercises: []service.PlannedExercise{plan()}},
	}}
	first, err := s.ReplaceWeek(t.Context(), "owner", "2026-10-26", input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ReplaceWeek(t.Context(), "owner", "2026-10-26", input)
	if err != nil || replay.Revision != first.Revision {
		t.Fatalf("replay changed revision: %+v %v", replay, err)
	}
	_, err = s.MoveWorkout(t.Context(), "owner", "2026-10-26", service.MoveWorkoutRequest{
		RequestID: requestID(4), ExpectedRevision: 1, WorkoutID: requestID(2), PlannedDate: "2026-10-28"})
	var conflict *service.Error
	if !errors.As(err, &conflict) || conflict.Code != "day_occupied" {
		t.Fatalf("expected occupied conflict: %v", err)
	}
	move := service.MoveWorkoutRequest{RequestID: requestID(5), ExpectedRevision: 1, WorkoutID: requestID(2), PlannedDate: "2026-10-27"}
	moved, err := s.MoveWorkout(t.Context(), "owner", "2026-10-26", move)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.MoveWorkout(t.Context(), "owner", "2026-10-26", move)
	if err != nil || again.Revision != 2 || moved.Workouts[0].ID != requestID(2) {
		t.Fatalf("move replay unstable: %+v %v", again, err)
	}
	move.PlannedDate = "2026-10-29"
	_, err = s.MoveWorkout(t.Context(), "owner", "2026-10-26", move)
	if !errors.As(err, &conflict) || conflict.Code != "idempotency_conflict" {
		t.Fatalf("expected request reuse rejection: %v", err)
	}
}

func activeSession() service.WorkoutSession {
	return service.WorkoutSession{ID: requestID(1), Title: "Верх", Weekday: 1, StartedAt: time.Now().UTC().Truncate(time.Second),
		Exercises: []service.SessionExercise{
			{ID: requestID(2), Exercise: plan().Exercise, RestSeconds: 90, RestAfterExerciseSeconds: 180, Sets: []service.LoggedSet{
				{ID: requestID(3), Reps: "10", Weight: "60", IsCompleted: true}, {ID: requestID(4), Reps: "draft", Weight: ""},
			}},
			{ID: requestID(5), Exercise: plan().Exercise, RestSeconds: 90, RestAfterExerciseSeconds: 180,
				Sets: []service.LoggedSet{{ID: requestID(6)}}},
		},
	}
}

func TestSetEffortSurvivesCommandsAndBlocksLegacyActiveAndHistoryWrites(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	session := activeSession()
	reserve := 0
	session.Exercises[0].Sets[0].RepsInReserve = &reserve
	saved, err := s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{
		RequestID: requestID(30), State: &service.SessionState{Session: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.SetActiveRest(t.Context(), "owner", service.ActiveRestRequest{
		RequestID: requestID(31), ExpectedRevision: saved.Revision, SessionID: session.ID,
		ExerciseID: session.Exercises[0].ID, RestSeconds: 120, RestAfterExerciseSeconds: 180,
	})
	if err != nil {
		t.Fatal(err)
	}
	skipped, err := s.SkipExercise(t.Context(), "owner", service.SkipExerciseRequest{
		RequestID: requestID(32), ExpectedRevision: changed.Revision, SessionID: session.ID,
		ExerciseID: session.Exercises[0].ID, IsSkipped: true,
	})
	if err != nil || skipped.State.Session.Exercises[0].Sets[0].RepsInReserve == nil {
		t.Fatalf("effort lost by server command: %v", err)
	}
	legacy := activeSession()
	legacy.StartedAt = session.StartedAt
	_, err = s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{
		RequestID: requestID(33), ExpectedRevision: skipped.Revision, State: &service.SessionState{Session: legacy},
	})
	var failure *service.Error
	if !errors.As(err, &failure) || failure.Code != "upgrade_required" {
		t.Fatalf("legacy write should require upgrade: %v", err)
	}
	finish := session.StartedAt.Add(time.Minute)
	legacy.FinishedAt = &finish
	_, err = s.AppendHistory(t.Context(), "owner", service.HistoryWrite{RequestID: requestID(34), Session: legacy})
	if !errors.As(err, &failure) || failure.Code != "upgrade_required" {
		t.Fatalf("legacy history should require upgrade: %v", err)
	}
	history, err := s.ListHistory(t.Context(), "owner")
	if err != nil || len(history) != 0 {
		t.Fatalf("rejected history was persisted: %v", err)
	}
	final, err := s.GetActive(t.Context(), "owner")
	if err != nil || final.Revision != skipped.Revision {
		t.Fatalf("rejected write changed active: %v", err)
	}
	cleared := -1
	final.State.Session.Exercises[0].Sets[0].RepsInReserve = &cleared
	updated, err := s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{
		RequestID: requestID(35), ExpectedRevision: final.Revision, State: final.State,
	})
	if err != nil {
		t.Fatalf("explicit assessment clearing rejected: %v", err)
	}
	completed := updated.State.Session
	completed.FinishedAt = &finish
	input := service.HistoryWrite{RequestID: requestID(36), Session: completed}
	for range 2 {
		if _, err := s.AppendHistory(t.Context(), "owner", input); err != nil {
			t.Fatal(err)
		}
	}
	history, err = s.ListHistory(t.Context(), "owner")
	if err != nil || len(history) != 1 || *history[0].Exercises[0].Sets[0].RepsInReserve != -1 {
		t.Fatalf("history effort did not round trip: %v", err)
	}
	completed.Exercises[0].Sets[0].RepsInReserve = nil
	_, err = s.AppendHistory(t.Context(), "owner", service.HistoryWrite{RequestID: requestID(37), Session: completed})
	if !errors.As(err, &failure) || failure.Code != "history_immutable" {
		t.Fatalf("history effort was mutable: %v", err)
	}
}

func TestSkipPreservesSetsAndRejectsWrongSession(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	state, err := s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{RequestID: requestID(10), State: &service.SessionState{Session: activeSession()}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SkipExercise(t.Context(), "owner", service.SkipExerciseRequest{RequestID: requestID(11), ExpectedRevision: state.Revision,
		SessionID: requestID(99), ExerciseID: requestID(2), IsSkipped: true})
	if err == nil {
		t.Fatal("wrong active session accepted")
	}
	skipped, err := s.SkipExercise(t.Context(), "owner", service.SkipExerciseRequest{RequestID: requestID(12), ExpectedRevision: state.Revision,
		SessionID: requestID(1), ExerciseID: requestID(2), IsSkipped: true})
	if err != nil {
		t.Fatal(err)
	}
	exercise := skipped.State.Session.Exercises[0]
	if !exercise.IsSkipped || !exercise.Sets[0].IsCompleted || exercise.Sets[1].Reps != "draft" || skipped.State.Session.CurrentExercise != 1 {
		t.Fatalf("skip damaged session: %+v", skipped)
	}
	returned, err := s.SkipExercise(t.Context(), "owner", service.SkipExerciseRequest{
		RequestID: requestID(13), ExpectedRevision: skipped.Revision,
		SessionID: requestID(1), ExerciseID: requestID(2), IsSkipped: false,
	})
	if err != nil || returned.State.Session.CurrentExercise != 0 || returned.State.Session.Exercises[0].IsSkipped {
		t.Fatalf("unskip did not return to exercise: %+v %v", returned, err)
	}
	returned.State.Session.Exercises[0].Sets[1].Reps = "8"
	returned.State.Session.Exercises[0].Sets[1].Weight = "60"
	returned.State.Session.Exercises[0].Sets[1].IsCompleted = true
	completed, err := s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{
		RequestID: requestID(14), ExpectedRevision: returned.Revision, State: returned.State,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SkipExercise(t.Context(), "owner", service.SkipExerciseRequest{
		RequestID: requestID(15), ExpectedRevision: completed.Revision,
		SessionID: requestID(1), ExerciseID: requestID(2), IsSkipped: true,
	})
	if err == nil {
		t.Fatal("fully completed exercise was skipped")
	}
}

func TestHistoryImmutableAndActiveIndependent(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	session := activeSession()
	_, err := s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{RequestID: requestID(10), State: &service.SessionState{Session: session}})
	if err != nil {
		t.Fatal(err)
	}
	finish := session.StartedAt.Add(time.Hour)
	session.FinishedAt = &finish
	input := service.HistoryWrite{RequestID: requestID(11), Session: session}
	if _, err := s.AppendHistory(t.Context(), "owner", input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendHistory(t.Context(), "owner", input); err != nil {
		t.Fatal(err)
	}
	history, err := s.ListHistory(t.Context(), "owner")
	if err != nil || len(history) != 1 {
		t.Fatalf("history replay duplicated: %v %v", history, err)
	}
	if history[0].Timing != nil {
		t.Fatal("legacy history acquired fabricated timing")
	}
	active, err := s.GetActive(t.Context(), "owner")
	if err != nil || active.State == nil {
		t.Fatal("history append cleared active")
	}
	input.RequestID = requestID(12)
	input.Session.Title = "Changed"
	_, err = s.AppendHistory(t.Context(), "owner", input)
	var immutable *service.Error
	if !errors.As(err, &immutable) || immutable.Code != "history_immutable" {
		t.Fatalf("history changed: %v", err)
	}
	other, err := s.ListHistory(t.Context(), "other")
	if err != nil || len(other) != 0 {
		t.Fatal("other owner history leaked")
	}
}

func TestProteinDateIsolationAndCAS(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	empty, err := s.GetProtein(t.Context(), "owner", "2026-10-26")
	if err != nil || empty.Revision != 0 || empty.MorningTakenAt != nil {
		t.Fatalf("empty protein state: %+v %v", empty, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	saved, err := s.ReplaceProtein(t.Context(), "owner", "2026-10-26", service.ProteinWrite{RequestID: requestID(1), MorningTakenAt: &now})
	if err != nil || saved.Revision != 1 || saved.MorningTakenAt == nil {
		t.Fatalf("save protein state: %+v %v", saved, err)
	}
	tomorrow, err := s.GetProtein(t.Context(), "owner", "2026-10-27")
	if err != nil || tomorrow.MorningTakenAt != nil {
		t.Fatal("protein state leaked across dates")
	}
	other, err := s.GetProtein(t.Context(), "other", "2026-10-26")
	if err != nil || other.MorningTakenAt != nil {
		t.Fatal("protein state leaked across users")
	}
	_, err = s.ReplaceProtein(t.Context(), "owner", "2026-10-26", service.ProteinWrite{RequestID: requestID(2)})
	var conflict *service.Error
	if !errors.As(err, &conflict) || conflict.Code != "revision_conflict" {
		t.Fatalf("stale protein write accepted: %v", err)
	}
}

func activeTiming(session service.WorkoutSession) *service.WorkoutTiming {
	exerciseID := session.Exercises[0].ID
	kind := "betweenSets"
	restStart := session.StartedAt.Add(10 * time.Second)
	deadline := session.StartedAt.Add(100 * time.Second)
	return &service.WorkoutTiming{Version: 1, TrackedFrom: session.StartedAt,
		Intervals: []service.WorkoutTimingInterval{
			{ID: requestID(71), Kind: "exercise", ExerciseID: &exerciseID, StartedAt: session.StartedAt, EndedAt: &restStart},
			{ID: requestID(72), Kind: "rest", ExerciseID: &exerciseID, RestKind: &kind, StartedAt: restStart, RestDeadline: &deadline},
		},
	}
}

func requireSameTiming(t *testing.T, want, actual *service.WorkoutTiming) {
	t.Helper()
	expected, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, got) {
		t.Fatalf("phone timing changed: want %s, got %s", expected, got)
	}
}

func TestActiveCommandsPreservePhoneTimingAndRejectLegacyDrop(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	session := activeSession()
	session.StartedAt = session.StartedAt.Add(-10 * time.Minute)
	session.Timing = activeTiming(session)
	deadline := session.Timing.Intervals[1].RestDeadline
	sourceID := session.Exercises[0].ID
	restKind := "betweenSets"
	initial := &service.SessionState{Session: session, RestUntil: deadline, RestKind: &restKind, RestExerciseID: &sourceID}
	saved, err := s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{RequestID: requestID(10), State: initial})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.SetActiveRest(t.Context(), "owner", service.ActiveRestRequest{
		RequestID: requestID(11), ExpectedRevision: saved.Revision, SessionID: session.ID,
		ExerciseID: sourceID, RestSeconds: 120, RestAfterExerciseSeconds: 240,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireSameTiming(t, session.Timing, changed.State.Session.Timing)
	if changed.State.RestUntil == nil || !changed.State.RestUntil.Equal(*deadline) {
		t.Fatal("rest settings changed captured timer")
	}
	skipped, err := s.SkipExercise(t.Context(), "owner", service.SkipExerciseRequest{
		RequestID: requestID(12), ExpectedRevision: changed.Revision, SessionID: session.ID, ExerciseID: sourceID, IsSkipped: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireSameTiming(t, session.Timing, skipped.State.Session.Timing)
	if skipped.State.RestUntil != nil {
		t.Fatal("skip did not clear operational timer")
	}
	if skipped.State.Session.Timing.Intervals[1].EndedAt != nil {
		t.Fatal("server fabricated a rest end time")
	}
	restored, err := s.SkipExercise(t.Context(), "owner", service.SkipExerciseRequest{
		RequestID: requestID(13), ExpectedRevision: skipped.Revision, SessionID: session.ID, ExerciseID: sourceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireSameTiming(t, session.Timing, restored.State.Session.Timing)
	legacy := *restored.State
	legacy.Session.Timing = nil
	body, err := json.Marshal(service.ActiveWrite{RequestID: requestID(14), ExpectedRevision: restored.Revision, State: &legacy})
	if err != nil {
		t.Fatal(err)
	}
	verify := func(context.Context, string, *http.Request) (*sdkauth.TokenInfo, error) {
		return &sdkauth.TokenInfo{UserID: "owner", Scopes: []string{"workout:write"}, Expiration: time.Now().Add(time.Minute)}, nil
	}
	handler := sdkauth.RequireBearerToken(verify, nil)(httpapi.New(s))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/v1/active", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer fixture")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("legacy drop status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Error service.Error `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "upgrade_required" {
		t.Fatalf("wrong error: %+v", response)
	}
	final, err := s.GetActive(t.Context(), "owner")
	if err != nil || final.Revision != restored.Revision {
		t.Fatalf("legacy drop changed active: %+v %v", final, err)
	}
	requireSameTiming(t, session.Timing, final.State.Session.Timing)
}

func TestTimedHistoryRoundTripsAndRemainsImmutable(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	session := activeSession()
	session.StartedAt = session.StartedAt.Add(-10 * time.Minute)
	session.Timing = activeTiming(session)
	deadline := *session.Timing.Intervals[1].RestDeadline
	finish := deadline.Add(20 * time.Second)
	session.FinishedAt = &finish
	session.Timing.Intervals[1].EndedAt = &deadline
	session.Timing.Intervals = append(session.Timing.Intervals, service.WorkoutTimingInterval{
		ID: requestID(73), Kind: "other", StartedAt: deadline, EndedAt: &finish,
	})
	input := service.HistoryWrite{RequestID: requestID(10), Session: session}
	if _, err := s.AppendHistory(t.Context(), "owner", input); err != nil {
		t.Fatal(err)
	}
	history, err := s.ListHistory(t.Context(), "owner")
	if err != nil || len(history) != 1 {
		t.Fatalf("timed history missing: %v", err)
	}
	requireSameTiming(t, session.Timing, history[0].Timing)
	if _, err := s.AppendHistory(t.Context(), "owner", input); err != nil {
		t.Fatal(err)
	}
	earlier := deadline.Add(-time.Second)
	input.Session.Timing.Intervals[1].EndedAt = &earlier
	input.RequestID = requestID(11)
	_, err = s.AppendHistory(t.Context(), "owner", input)
	var immutable *service.Error
	if !errors.As(err, &immutable) || immutable.Code != "history_immutable" {
		t.Fatalf("timed history overwritten: %v", err)
	}
}
