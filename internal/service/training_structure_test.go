package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func structureFixtureID(index int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", index)
}

func supersetSessionFixture() WorkoutSession {
	a, b, c, d := structureFixtureID(10), structureFixtureID(11), structureFixtureID(12), structureFixtureID(13)
	return WorkoutSession{
		ID: structureFixtureID(1), Weekday: 1, StartedAt: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC),
		StructureVersion: 1, Context: &WorkoutPlanContext{Variant: "A", LoadType: "heavy", Difficulty: "hard"},
		Exercises: []SessionExercise{
			{ID: structureFixtureID(2), Exercise: Exercise{ID: structureFixtureID(4), Name: "Press"},
				Sets: []LoggedSet{{ID: a, Reps: "8", Weight: "20"}, {ID: b}}, RestSeconds: 60, RestAfterExerciseSeconds: 90},
			{ID: structureFixtureID(3), Exercise: Exercise{ID: structureFixtureID(5), Name: "Row"},
				Sets: []LoggedSet{{ID: c, Reps: "8", Weight: "20"}, {ID: d}}, RestSeconds: 60, RestAfterExerciseSeconds: 90},
		},
		Supersets: []SessionSuperset{{ID: structureFixtureID(30), ExerciseIDs: []string{structureFixtureID(2), structureFixtureID(3)},
			RestSeconds: 90, Rounds: []SupersetRound{
				{ID: structureFixtureID(40), Ordinal: 1, SetIDs: []*string{&a, &c}},
				{ID: structureFixtureID(41), Ordinal: 2, SetIDs: []*string{&b, &d}},
			}}},
	}
}

func cycleDocumentFixture() TrainingCycleDocument {
	weeks := []TrainingCycleWeek{}
	for number := 0; number < 2; number++ {
		days := emptyTemplate().Days
		days[0].Context = &WorkoutPlanContext{Variant: "A", LoadType: "regular", Difficulty: "moderate"}
		days[0].Exercises = []PlannedExercise{
			{ID: structureFixtureID(2), Exercise: Exercise{ID: structureFixtureID(4), Name: "Press"},
				SetCount: 2, TargetReps: "8–12", RestSeconds: 60, RestAfterExerciseSeconds: 90},
			{ID: structureFixtureID(3), Exercise: Exercise{ID: structureFixtureID(5), Name: "Row"},
				SetCount: 2, TargetReps: "8–12", RestSeconds: 60, RestAfterExerciseSeconds: 90},
		}
		days[0].Supersets = []PlannedSuperset{{ID: structureFixtureID(30),
			ExerciseIDs: []string{structureFixtureID(2), structureFixtureID(3)}, RestSeconds: 90}}
		weeks = append(weeks, TrainingCycleWeek{ID: structureFixtureID(50 + number), Days: days})
	}
	return TrainingCycleDocument{Version: 1, Plan: &TrainingCyclePlan{ID: structureFixtureID(60),
		StartWeek: "2026-11-02", Timezone: "Europe/Moscow", Weeks: weeks}}
}

func TestTrainingCycleRejectsInvalidPlansAndPreservesExplicitClear(t *testing.T) {
	t.Parallel()
	if err := cycleDocumentFixture().validate(); err != nil {
		t.Fatal(err)
	}
	if err := (TrainingCycleDocument{Version: 1}).validate(); err != nil {
		t.Fatal("explicit plan removal rejected", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*TrainingCycleDocument)
	}{
		{"future version", func(d *TrainingCycleDocument) { d.Version = 2 }},
		{"missing version", func(d *TrainingCycleDocument) { d.Version = 0 }},
		{"one week", func(d *TrainingCycleDocument) { d.Plan.Weeks = d.Plan.Weeks[:1] }},
		{"thirteen weeks", func(d *TrainingCycleDocument) { d.Plan.Weeks = make([]TrainingCycleWeek, 13) }},
		{"duplicate phase", func(d *TrainingCycleDocument) { d.Plan.Weeks[1].ID = d.Plan.Weeks[0].ID }},
		{"missing day", func(d *TrainingCycleDocument) { d.Plan.Weeks[0].Days = d.Plan.Weeks[0].Days[:6] }},
		{"duplicate weekday", func(d *TrainingCycleDocument) { d.Plan.Weeks[0].Days[1].Weekday = 1 }},
		{"host timezone", func(d *TrainingCycleDocument) { d.Plan.Timezone = "Local" }},
		{"missing timezone", func(d *TrainingCycleDocument) { d.Plan.Timezone = "" }},
		{"not monday", func(d *TrainingCycleDocument) { d.Plan.StartWeek = "2026-11-03" }},
		{"old date", func(d *TrainingCycleDocument) { d.Plan.StartWeek = "1899-12-25" }},
		{"end out of range", func(d *TrainingCycleDocument) { d.Plan.StartWeek = "9999-12-27" }},
		{"missing exercises", func(d *TrainingCycleDocument) { d.Plan.Weeks[0].Days[1].Exercises = nil }},
		{"unequal rounds", func(d *TrainingCycleDocument) { d.Plan.Weeks[0].Days[0].Exercises[1].SetCount = 3 }},
		{"unknown variant", func(d *TrainingCycleDocument) { d.Plan.Weeks[0].Days[0].Context.Variant = "D" }},
		{"unknown load", func(d *TrainingCycleDocument) { d.Plan.Weeks[0].Days[0].Context.LoadType = "automatic" }},
		{"calendar snapshot", func(d *TrainingCycleDocument) {
			d.Plan.Weeks[0].Days[0].Context.Cycle = &CycleWeekReference{CycleID: d.Plan.ID, WeekID: d.Plan.Weeks[0].ID, WeekNumber: 1}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := cycleDocumentFixture()
			tc.mutate(&doc)
			if err := doc.validate(); err == nil {
				t.Fatal("invalid cycle accepted")
			}
		})
	}
}

func TestSupersetSessionValidatesReferencesSlotsAndFormat(t *testing.T) {
	t.Parallel()
	if err := validateSession(supersetSessionFixture(), false); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*WorkoutSession)
	}{
		{"undeclared structure", func(s *WorkoutSession) { s.StructureVersion = 0 }},
		{"future structure", func(s *WorkoutSession) { s.StructureVersion = 2 }},
		{"unknown member", func(s *WorkoutSession) { s.Supersets[0].ExerciseIDs[1] = structureFixtureID(99) }},
		{"duplicate member", func(s *WorkoutSession) { s.Supersets[0].ExerciseIDs[1] = s.Supersets[0].ExerciseIDs[0] }},
		{"unknown set", func(s *WorkoutSession) { *s.Supersets[0].Rounds[0].SetIDs[0] = structureFixtureID(99) }},
		{"duplicate set", func(s *WorkoutSession) { s.Supersets[0].Rounds[1].SetIDs[0] = s.Supersets[0].Rounds[0].SetIDs[0] }},
		{"wrong slot", func(s *WorkoutSession) {
			s.Supersets[0].Rounds[0].SetIDs[0], s.Supersets[0].Rounds[0].SetIDs[1] = s.Supersets[0].Rounds[0].SetIDs[1], s.Supersets[0].Rounds[0].SetIDs[0]
		}},
		{"duplicate round", func(s *WorkoutSession) { s.Supersets[0].Rounds[1].ID = s.Supersets[0].Rounds[0].ID }},
		{"reordered ordinal", func(s *WorkoutSession) { s.Supersets[0].Rounds[1].Ordinal = 1 }},
		{"long rest", func(s *WorkoutSession) { s.Supersets[0].RestSeconds = 601 }},
		{"closed pending", func(s *WorkoutSession) { s.Supersets[0].IsClosed = true }},
		{"empty round", func(s *WorkoutSession) { s.Supersets[0].Rounds[0].SetIDs = []*string{nil, nil} }},
		{"short round", func(s *WorkoutSession) { s.Supersets[0].Rounds[0].SetIDs = s.Supersets[0].Rounds[0].SetIDs[:1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			session := supersetSessionFixture()
			tc.mutate(&session)
			if err := validateSession(session, false); err == nil {
				t.Fatal("invalid superset accepted")
			}
		})
	}
}

func TestSupersetReplacementAndClosePreserveCompletedRound(t *testing.T) {
	t.Parallel()
	previous := supersetSessionFixture()
	previous.Exercises[0].Sets[0].IsCompleted = true
	encoded, _ := json.Marshal(previous)
	var replaced WorkoutSession
	if err := json.Unmarshal(encoded, &replaced); err != nil {
		t.Fatal(err)
	}
	replacementSetID := structureFixtureID(70)
	replaced.Exercises[0].IsSkipped = true
	replaced.Exercises = append(replaced.Exercises, SessionExercise{
		ID: structureFixtureID(71), Exercise: Exercise{ID: structureFixtureID(72), Name: "Replacement"},
		Sets: []LoggedSet{{ID: replacementSetID}},
	})
	replaced.Supersets[0].ExerciseIDs[0] = structureFixtureID(71)
	replaced.Supersets[0].Rounds[1].SetIDs[0] = &replacementSetID
	if err := validateSession(replaced, false); err != nil {
		t.Fatalf("replacement lost old results: %v", err)
	}
	if err := replaced.ValidateSetPreservation(previous, nil); err != nil {
		t.Fatal(err)
	}
	replaced.Exercises[1].IsSkipped = true
	replaced.closeSupersetsAfterSkip()
	if err := validateSession(replaced, false); err != nil {
		t.Fatal(err)
	}
	if err := replaced.ValidateSetPreservation(previous, nil); err != nil {
		t.Fatal(err)
	}
	group := replaced.Supersets[0]
	if !group.IsClosed || len(group.Rounds) != 1 || group.Rounds[0].SetIDs[1] != nil {
		t.Fatalf("future links were not removed: %+v", group)
	}
	if *group.Rounds[0].SetIDs[0] != previous.Exercises[0].Sets[0].ID || len(replaced.Exercises[1].Sets) != 2 {
		t.Fatal("closing group changed recorded or draft sets")
	}
	if !bytes.Equal(encoded, mustStructureJSON(t, previous)) {
		t.Fatal("closing group mutated the earlier snapshot")
	}
}

func TestSupersetCompletedLinksCannotDisappearOrMove(t *testing.T) {
	t.Parallel()
	previous := supersetSessionFixture()
	previous.Exercises[0].Sets[0].IsCompleted = true
	for _, tc := range []struct {
		name   string
		mutate func(*WorkoutSession)
		code   string
	}{
		{"legacy omission", func(s *WorkoutSession) { s.StructureVersion = 0; s.Context = nil; s.Supersets = nil }, "upgrade_required"},
		{"group removal", func(s *WorkoutSession) { s.Supersets = nil }, "superset_conflict"},
		{"set removal", func(s *WorkoutSession) { s.Supersets[0].Rounds[0].SetIDs[0] = nil }, "superset_conflict"},
		{"ordinal change", func(s *WorkoutSession) { s.Supersets[0].Rounds[0].Ordinal = 5 }, "superset_conflict"},
		{"round change", func(s *WorkoutSession) { s.Supersets[0].Rounds[0].ID = structureFixtureID(100) }, "superset_conflict"},
		{"source exercise change", func(s *WorkoutSession) { s.Exercises[0].Exercise.ID = structureFixtureID(100) }, "superset_conflict"},
		{"source card change", func(s *WorkoutSession) {
			s.Exercises[0].ID = structureFixtureID(100)
			s.Supersets[0].ExerciseIDs[0] = structureFixtureID(100)
		}, "superset_conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var current WorkoutSession
			if err := json.Unmarshal(mustStructureJSON(t, previous), &current); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&current)
			err := current.ValidateSetPreservation(previous, nil)
			var failure *Error
			if !errors.As(err, &failure) || failure.Code != tc.code {
				t.Fatalf("protection=%v, want %s", err, tc.code)
			}
		})
	}
}

func mustStructureJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTrainingCycleStoredFutureAndCorruptFormatsCannotBeCleared(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`{"revision":8,"document":{"version":2,"privateField":"keep"}}`,
		`{"revision":8,"document":{"version":1,"plan":{}}}`,
	} {
		repo := &storedDocumentFixture{resource: Resource{Revision: 8, Payload: []byte(payload)}}
		s := New(repo)
		if _, err := s.GetTrainingCycle(t.Context(), "owner"); err == nil {
			t.Fatal("bad stored cycle returned")
		}
		_, err := s.ReplaceTrainingCycle(t.Context(), "owner", TrainingCycleWrite{
			RequestID: structureFixtureID(100), ExpectedRevision: 8, Document: TrainingCycleDocument{Version: 1},
		})
		if err == nil || string(repo.resource.Payload) != payload || repo.resource.Revision != 8 {
			t.Fatal("invalid existing cycle overwritten", err)
		}
	}
}

func TestCycleWriteResultDoesNotAliasRequest(t *testing.T) {
	t.Parallel()
	repo := &storedDocumentFixture{}
	s := New(repo)
	input := TrainingCycleWrite{RequestID: structureFixtureID(100), Document: cycleDocumentFixture()}
	result, err := s.ReplaceTrainingCycle(t.Context(), "owner", input)
	if err != nil {
		t.Fatal(err)
	}
	result.Document.Plan.Weeks[0].Days[0].Exercises[0].SetCount = 9
	if input.Document.Plan.Weeks[0].Days[0].Exercises[0].SetCount != 2 {
		t.Fatal("response aliases request")
	}
	stored, err := s.GetTrainingCycle(t.Context(), "owner")
	if err != nil || stored.Document.Plan.Weeks[0].Days[0].Exercises[0].SetCount != 2 {
		t.Fatal("response mutated storage", err)
	}
}

func TestTemplateVersionProtectsEvenRemovalOfAllNewFields(t *testing.T) {
	t.Parallel()
	doc := cycleDocumentFixture()
	previous := Template{Revision: 2, StructureVersion: 1, Days: doc.Plan.Weeks[0].Days}
	repo := &storedDocumentFixture{resource: Resource{Revision: 2, Payload: mustStructureJSON(t, previous)}}
	s := New(repo)
	legacy := TemplateWrite{RequestID: structureFixtureID(100), ExpectedRevision: 2, Days: emptyTemplate().Days}
	_, err := s.ReplaceTemplate(t.Context(), "owner", legacy)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "upgrade_required" {
		t.Fatalf("legacy overwrite accepted: %v", err)
	}
	legacy.StructureVersion = 1
	result, err := s.ReplaceTemplate(t.Context(), "owner", legacy)
	if err != nil || result.StructureVersion != 1 || result.Revision != 3 {
		t.Fatal("explicit new-format clear rejected", err)
	}
	legacy.StructureVersion = 0
	legacy.ExpectedRevision = 3
	if _, err := s.ReplaceTemplate(t.Context(), "owner", legacy); err == nil {
		t.Fatal("format marker was downgraded")
	}
}

func TestLegacyWorkoutJSONShapeRemainsUnchanged(t *testing.T) {
	t.Parallel()
	legacy := supersetSessionFixture()
	legacy.StructureVersion = 0
	legacy.Context = nil
	legacy.Supersets = nil
	data := mustStructureJSON(t, legacy)
	for _, field := range []string{"structureVersion", "supersets", "context"} {
		if strings.Contains(string(data), `"`+field+`"`) {
			t.Fatalf("legacy payload gained %s", field)
		}
	}
	var decoded WorkoutSession
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(legacy, decoded) {
		t.Fatal("legacy roundtrip changed", err)
	}
}
