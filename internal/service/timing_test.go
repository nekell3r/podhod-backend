package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func timedWorkoutSession() WorkoutSession {
	start := time.Date(2026, time.October, 26, 10, 0, 0, 0, time.UTC)
	finish := start.Add(10 * time.Minute)
	exerciseID := "abcdef12-0000-4000-8000-000000000002"
	restKind := "betweenSets"
	workEnd := start.Add(time.Minute)
	restEnd := start.Add(150 * time.Second)
	otherEnd := start.Add(3 * time.Minute)
	return WorkoutSession{
		ID: "00000000-0000-4000-8000-000000000001", Title: "Timing fixture", Weekday: 1,
		StartedAt: start, FinishedAt: &finish, Exercises: []SessionExercise{{
			ID: exerciseID, Exercise: Exercise{ID: "00000000-0000-4000-8000-000000000003", Name: "Жим"},
			Sets: []LoggedSet{{ID: "00000000-0000-4000-8000-000000000004", Reps: "10", Weight: "60", IsCompleted: true}},
		}},
		Timing: &WorkoutTiming{Version: 1, TrackedFrom: start, Intervals: []WorkoutTimingInterval{
			{ID: "00000000-0000-4000-8000-000000000011", Kind: "exercise", ExerciseID: &exerciseID, StartedAt: start, EndedAt: &workEnd},
			{ID: "00000000-0000-4000-8000-000000000012", Kind: "rest", ExerciseID: &exerciseID, RestKind: &restKind,
				StartedAt: workEnd, EndedAt: &restEnd, RestDeadline: &restEnd},
			{ID: "00000000-0000-4000-8000-000000000013", Kind: "other", StartedAt: restEnd, EndedAt: &otherEnd},
			{ID: "00000000-0000-4000-8000-000000000014", Kind: "exercise", ExerciseID: &exerciseID,
				StartedAt: otherEnd, EndedAt: &finish},
		}},
	}
}

func TestWorkoutTimingValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		valid  bool
		change func(*WorkoutSession)
	}{
		{name: "closed history", valid: true},
		{name: "exercise identity ignores UUID case", valid: true, change: func(s *WorkoutSession) {
			s.Exercises[0].ID = strings.ToUpper(s.Exercises[0].ID)
		}},
		{name: "zero elapsed with empty intervals", valid: true, change: func(s *WorkoutSession) {
			finish := s.StartedAt
			s.FinishedAt = &finish
			s.Timing.Intervals = []WorkoutTimingInterval{}
		}},
		{name: "zero length closed interval", valid: true, change: func(s *WorkoutSession) {
			end := s.Timing.Intervals[0].StartedAt
			s.Timing.Intervals[0].EndedAt = &end
		}},
		{name: "legacy history remains unknown", valid: true, change: func(s *WorkoutSession) { s.Timing = nil }},
		{name: "active last interval open", valid: true, change: func(s *WorkoutSession) {
			s.FinishedAt = nil
			s.Timing.Intervals[3].EndedAt = nil
		}},
		{name: "gap remains untracked", valid: true, change: func(s *WorkoutSession) {
			s.Timing.Intervals[1].StartedAt = s.Timing.Intervals[1].StartedAt.Add(time.Second)
		}},
		{name: "empty explicit timeline remains untracked", valid: true, change: func(s *WorkoutSession) {
			s.Timing.Intervals = []WorkoutTimingInterval{}
		}},
		{name: "manual rest stop before captured deadline", valid: true, change: func(s *WorkoutSession) {
			end := s.Timing.Intervals[1].StartedAt.Add(time.Second)
			s.Timing.Intervals[1].EndedAt = &end
		}},
		{name: "unknown version", change: func(s *WorkoutSession) { s.Timing.Version = 2 }},
		{name: "tracking precedes session", change: func(s *WorkoutSession) {
			s.Timing.TrackedFrom = s.StartedAt.Add(-time.Second)
		}},
		{name: "tracking follows finish", change: func(s *WorkoutSession) {
			s.Timing.TrackedFrom = s.FinishedAt.Add(time.Second)
		}},
		{name: "missing intervals array", change: func(s *WorkoutSession) { s.Timing.Intervals = nil }},
		{name: "overlap", change: func(s *WorkoutSession) {
			s.Timing.Intervals[1].StartedAt = s.Timing.Intervals[0].StartedAt
		}},
		{name: "unordered intervals", change: func(s *WorkoutSession) {
			s.Timing.Intervals[1], s.Timing.Intervals[2] = s.Timing.Intervals[2], s.Timing.Intervals[1]
		}},
		{name: "negative duration", change: func(s *WorkoutSession) {
			end := s.Timing.Intervals[3].StartedAt.Add(-time.Second)
			s.Timing.Intervals[3].EndedAt = &end
		}},
		{name: "interval follows finish", change: func(s *WorkoutSession) {
			end := s.FinishedAt.Add(time.Second)
			s.Timing.Intervals[3].EndedAt = &end
		}},
		{name: "open history", change: func(s *WorkoutSession) { s.Timing.Intervals[3].EndedAt = nil }},
		{name: "nonfinal open active interval", change: func(s *WorkoutSession) {
			s.FinishedAt = nil
			s.Timing.Intervals[1].EndedAt = nil
		}},
		{name: "duplicate interval id", change: func(s *WorkoutSession) {
			s.Timing.Intervals[1].ID = s.Timing.Intervals[0].ID
		}},
		{name: "invalid interval id", change: func(s *WorkoutSession) { s.Timing.Intervals[0].ID = "invalid" }},
		{name: "unknown kind", change: func(s *WorkoutSession) { s.Timing.Intervals[2].Kind = "physical_work" }},
		{name: "unknown exercise", change: func(s *WorkoutSession) {
			id := "00000000-0000-4000-8000-000000000099"
			s.Timing.Intervals[0].ExerciseID = &id
		}},
		{name: "missing exercise", change: func(s *WorkoutSession) { s.Timing.Intervals[0].ExerciseID = nil }},
		{name: "exercise id on other", change: func(s *WorkoutSession) {
			s.Timing.Intervals[2].ExerciseID = s.Timing.Intervals[0].ExerciseID
		}},
		{name: "missing rest kind", change: func(s *WorkoutSession) { s.Timing.Intervals[1].RestKind = nil }},
		{name: "unknown rest kind", change: func(s *WorkoutSession) {
			kind := "unknown"
			s.Timing.Intervals[1].RestKind = &kind
		}},
		{name: "missing captured rest deadline", change: func(s *WorkoutSession) {
			s.Timing.Intervals[1].RestDeadline = nil
		}},
		{name: "deadline precedes rest", change: func(s *WorkoutSession) {
			deadline := s.Timing.Intervals[1].StartedAt.Add(-time.Second)
			s.Timing.Intervals[1].RestDeadline = &deadline
		}},
		{name: "rest exceeds deadline", change: func(s *WorkoutSession) {
			deadline := s.Timing.Intervals[1].EndedAt.Add(-time.Second)
			s.Timing.Intervals[1].RestDeadline = &deadline
		}},
		{name: "deadline on exercise", change: func(s *WorkoutSession) {
			s.Timing.Intervals[0].RestDeadline = s.Timing.Intervals[1].RestDeadline
		}},
		{name: "rest kind on other", change: func(s *WorkoutSession) {
			s.Timing.Intervals[2].RestKind = s.Timing.Intervals[1].RestKind
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := timedWorkoutSession()
			if tc.change != nil {
				tc.change(&session)
			}
			err := validateSession(session, session.FinishedAt != nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestLegacySessionJSONDoesNotFabricateTiming(t *testing.T) {
	t.Parallel()
	session := timedWorkoutSession()
	session.Timing = nil
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"timing"`)) {
		t.Fatal("legacy timing was fabricated")
	}
	var decoded WorkoutSession
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Timing != nil {
		t.Fatal("missing timing did not remain unknown")
	}
	if err := validateSession(decoded, true); err != nil {
		t.Fatal(err)
	}
}
