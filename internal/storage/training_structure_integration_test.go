//go:build integration

package storage_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"podhod/server/internal/service"
)

func trainingCycleFixture() service.TrainingCycleDocument {
	weeks := []service.TrainingCycleWeek{}
	for number := range 2 {
		days := []service.TrainingDay{}
		for weekday := 1; weekday <= 7; weekday++ {
			days = append(days, service.TrainingDay{Weekday: weekday, Exercises: []service.PlannedExercise{}})
		}
		days[0].Exercises = []service.PlannedExercise{plan()}
		days[0].Context = &service.WorkoutPlanContext{Variant: "A", LoadType: "heavy", Difficulty: "hard"}
		weeks = append(weeks, service.TrainingCycleWeek{ID: requestID(byte(100 + number)), Days: days})
	}
	return service.TrainingCycleDocument{Version: 1, Plan: &service.TrainingCyclePlan{
		ID: requestID(102), StartWeek: "2026-11-02", Timezone: "Europe/Moscow", Repeats: true, Weeks: weeks,
	}}
}

func TestTrainingCycleOwnerCASReceiptsAndExplicitRemoval(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	input := service.TrainingCycleWrite{RequestID: requestID(110), Document: trainingCycleFixture()}
	first, err := s.ReplaceTrainingCycle(t.Context(), "owner", input)
	if err != nil || first.Revision != 1 {
		t.Fatalf("first cycle: %+v %v", first, err)
	}
	other, err := s.GetTrainingCycle(t.Context(), "other")
	if err != nil || other.Revision != 0 || other.Document.Plan != nil || other.Document.Version != 1 {
		t.Fatalf("foreign cycle exposed: %+v %v", other, err)
	}
	if _, err := s.ReplaceTrainingCycle(t.Context(), "other", input); err != nil {
		t.Fatal("owner-scoped request ID could not be reused", err)
	}
	changed := input
	changed.Document = trainingCycleFixture()
	changed.Document.Plan.Repeats = false
	_, err = s.ReplaceTrainingCycle(t.Context(), "owner", changed)
	var failure *service.Error
	if !errors.As(err, &failure) || failure.Code != "idempotency_conflict" {
		t.Fatal("request ID accepted a different cycle", err)
	}
	clear := service.TrainingCycleWrite{RequestID: requestID(111), ExpectedRevision: 1,
		Document: service.TrainingCycleDocument{Version: 1}}
	removed, err := s.ReplaceTrainingCycle(t.Context(), "owner", clear)
	if err != nil || removed.Revision != 2 || removed.Document.Plan != nil {
		t.Fatal("explicit removal failed", err)
	}
	receipt, err := s.ReplaceTrainingCycle(t.Context(), "owner", input)
	if err != nil || !reflect.DeepEqual(receipt, first) {
		t.Fatal("old exact retry lost receipt", err)
	}
	input.RequestID = requestID(112)
	_, err = s.ReplaceTrainingCycle(t.Context(), "owner", input)
	if !errors.As(err, &failure) || failure.Code != "revision_conflict" || failure.CurrentRevision != 2 {
		t.Fatal("stale cycle resurrected deleted plan", err)
	}
	current, err := s.GetTrainingCycle(t.Context(), "owner")
	if err != nil || !reflect.DeepEqual(current, removed) {
		t.Fatal("receipt rewrote latest cycle", err)
	}
}

func TestTrainingCycleConcurrentWritesHaveOneWinner(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	results := make(chan error, 4)
	var wg sync.WaitGroup
	for index := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ReplaceTrainingCycle(t.Context(), "owner", service.TrainingCycleWrite{
				RequestID: requestID(byte(120 + index)), Document: trainingCycleFixture(),
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
			continue
		}
		var failure *service.Error
		if !errors.As(err, &failure) || failure.Code != "revision_conflict" || failure.CurrentRevision != 1 {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("cycle winners=%d, want 1", winners)
	}
}

func TestCycleChangesLeaveTemplateWeekActiveAndHistorySnapshotsIndependent(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	doc := trainingCycleFixture()
	template, err := s.ReplaceTemplate(t.Context(), "owner", service.TemplateWrite{
		RequestID: requestID(130), StructureVersion: 1, Days: doc.Plan.Weeks[0].Days,
	})
	if err != nil {
		t.Fatal(err)
	}
	context := *doc.Plan.Weeks[0].Days[0].Context
	context.Cycle = &service.CycleWeekReference{CycleID: doc.Plan.ID, WeekID: doc.Plan.Weeks[0].ID, WeekNumber: 1}
	week, err := s.ReplaceWeek(t.Context(), "owner", "2026-11-02", service.WeekWrite{
		RequestID: requestID(131), StructureVersion: 1, Timezone: "Europe/Moscow",
		Workouts: []service.ScheduledWorkout{{ID: requestID(132), PlannedDate: "2026-11-02",
			Title: "Snapshot", Exercises: []service.PlannedExercise{plan()}, Context: &context}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := activeSession()
	session.StructureVersion = 1
	session.Context = &context
	active, err := s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{
		RequestID: requestID(133), State: &service.SessionState{Session: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := session.StartedAt.Add(time.Minute)
	session.FinishedAt = &finished
	history, err := s.AppendHistory(t.Context(), "owner", service.HistoryWrite{RequestID: requestID(134), Session: session})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ReplaceTrainingCycle(t.Context(), "owner", service.TrainingCycleWrite{RequestID: requestID(135), Document: doc})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ReplaceTrainingCycle(t.Context(), "owner", service.TrainingCycleWrite{RequestID: requestID(136),
		ExpectedRevision: 1, Document: service.TrainingCycleDocument{Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	gotTemplate, err := s.GetTemplate(t.Context(), "owner")
	if err != nil || !reflect.DeepEqual(gotTemplate, template) {
		t.Fatal("cycle changed template", err)
	}
	gotWeek, err := s.GetWeek(t.Context(), "owner", "2026-11-02", "UTC")
	if err != nil || !reflect.DeepEqual(gotWeek, week) {
		t.Fatal("cycle changed materialized week", err)
	}
	gotActive, err := s.GetActive(t.Context(), "owner")
	if err != nil || !reflect.DeepEqual(gotActive, active) {
		t.Fatal("cycle changed active session", err)
	}
	gotHistory, err := s.ListHistory(t.Context(), "owner")
	if err != nil || len(gotHistory) != 1 || !reflect.DeepEqual(gotHistory[0], history) {
		t.Fatal("cycle changed finished snapshot", err)
	}
	_, err = s.ReplaceTemplate(t.Context(), "owner", service.TemplateWrite{ExpectedRevision: 1,
		RequestID: requestID(137), Days: trainingCycleFixture().Plan.Weeks[1].Days})
	var failure *service.Error
	if !errors.As(err, &failure) || failure.Code != "upgrade_required" {
		t.Fatal("legacy template writer dropped structure", err)
	}
	_, err = s.ReplaceWeek(t.Context(), "owner", "2026-11-02", service.WeekWrite{ExpectedRevision: 1,
		RequestID: requestID(138), Timezone: "Europe/Moscow", Workouts: []service.ScheduledWorkout{}})
	if !errors.As(err, &failure) || failure.Code != "upgrade_required" {
		t.Fatal("legacy week writer dropped all structured workouts", err)
	}
	moved, err := s.MoveWorkout(t.Context(), "owner", "2026-11-02", service.MoveWorkoutRequest{
		ExpectedRevision: 1, RequestID: requestID(139), WorkoutID: week.Workouts[0].ID, PlannedDate: "2026-11-03",
	})
	if err != nil || moved.StructureVersion != 1 || !reflect.DeepEqual(moved.Workouts[0].Context, week.Workouts[0].Context) {
		t.Fatal("narrow calendar move lost structure", err)
	}
}

func TestSupersetActiveCommandsAndHistoryPreserveCompletedRoundLinks(t *testing.T) {
	t.Parallel()
	s := databaseService(t)
	session := activeSession()
	session.StructureVersion = 1
	reserve := 2
	session.Exercises[0].Sets[0].RepsInReserve = &reserve
	a, b := session.Exercises[0].Sets[0].ID, session.Exercises[1].Sets[0].ID
	session.Supersets = []service.SessionSuperset{{ID: requestID(140),
		ExerciseIDs: []string{session.Exercises[0].ID, session.Exercises[1].ID}, RestSeconds: 90,
		Rounds: []service.SupersetRound{{ID: requestID(141), Ordinal: 1, SetIDs: []*string{&a, &b}}},
	}}
	active, err := s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{RequestID: requestID(142),
		State: &service.SessionState{Session: session}})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.SetActiveRest(t.Context(), "owner", service.ActiveRestRequest{
		RequestID: requestID(143), ExpectedRevision: active.Revision, SessionID: session.ID,
		ExerciseID: session.Exercises[0].ID, RestSeconds: 120, RestAfterExerciseSeconds: 180,
	})
	if err != nil || !reflect.DeepEqual(changed.State.Session.Supersets, session.Supersets) {
		t.Fatal("rest command lost superset", err)
	}
	closed, err := s.SkipExercise(t.Context(), "owner", service.SkipExerciseRequest{
		RequestID: requestID(144), ExpectedRevision: changed.Revision, SessionID: session.ID,
		ExerciseID: session.Exercises[1].ID, IsSkipped: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	group := closed.State.Session.Supersets[0]
	if !group.IsClosed || group.Rounds[0].SetIDs[1] != nil || *group.Rounds[0].SetIDs[0] != a ||
		!reflect.DeepEqual(closed.State.Session.Exercises[0].Sets, session.Exercises[0].Sets) {
		t.Fatal("skip lost completed round, reserve or drafts")
	}
	legacy := *closed.State
	legacy.Session.StructureVersion = 0
	legacy.Session.Supersets = nil
	_, err = s.ReplaceActive(t.Context(), "owner", service.ActiveWrite{RequestID: requestID(145),
		ExpectedRevision: closed.Revision, State: &legacy})
	var failure *service.Error
	if !errors.As(err, &failure) || failure.Code != "upgrade_required" {
		t.Fatal("legacy active write lost superset", err)
	}
	finished := session.StartedAt.Add(time.Minute)
	legacy.Session.FinishedAt = &finished
	_, err = s.AppendHistory(t.Context(), "owner", service.HistoryWrite{RequestID: requestID(146), Session: legacy.Session})
	if !errors.As(err, &failure) || failure.Code != "upgrade_required" {
		t.Fatal("legacy history bypassed active structure guard", err)
	}
	legacy.Session.StructureVersion = 1
	_, err = s.AppendHistory(t.Context(), "owner", service.HistoryWrite{RequestID: requestID(147), Session: legacy.Session})
	if !errors.As(err, &failure) || failure.Code != "superset_conflict" {
		t.Fatal("new-format history dropped a completed group", err)
	}
	final := closed.State.Session
	final.FinishedAt = &finished
	input := service.HistoryWrite{RequestID: requestID(148), Session: final}
	for range 2 {
		if _, err := s.AppendHistory(t.Context(), "owner", input); err != nil {
			t.Fatal("protected finished retry failed", err)
		}
	}
	final.Context = &service.WorkoutPlanContext{Variant: "B", LoadType: "light", Difficulty: "easy"}
	input.Session = final
	input.RequestID = requestID(149)
	_, err = s.AppendHistory(t.Context(), "owner", input)
	if !errors.As(err, &failure) || failure.Code != "history_immutable" {
		t.Fatal("finished workout plan metadata changed", err)
	}
}
