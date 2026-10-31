package service

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestRepetitionsInReserveValidationAndJSON(t *testing.T) {
	t.Parallel()
	for _, value := range []int{-2, -1, 0, 1, 2, 3, 4, 5, 6} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			finish := time.Now()
			session := WorkoutSession{
				ID: "00000000-0000-4000-8000-000000000001", Weekday: 1,
				StartedAt: finish.Add(-time.Hour), FinishedAt: &finish,
				Exercises: []SessionExercise{{
					ID:       "00000000-0000-4000-8000-000000000002",
					Exercise: Exercise{ID: "00000000-0000-4000-8000-000000000003", Name: "Жим"},
					Sets: []LoggedSet{{
						ID: "00000000-0000-4000-8000-000000000004", Reps: "10", Weight: "60",
						IsCompleted: true, RepsInReserve: &value,
					}},
				}},
			}
			valid := value >= -1 && value <= 5
			if err := validateSession(session, true); (err == nil) != valid {
				t.Fatalf("reserve=%d valid=%v error=%v", value, valid, err)
			}
			session.Exercises[0].Sets[0].IsCompleted = false
			session.FinishedAt = nil
			if err := validateSession(session, false); (err == nil) != valid {
				t.Fatalf("draft reserve=%d valid=%v error=%v", value, valid, err)
			}
			encoded, err := json.Marshal(session)
			if err != nil {
				t.Fatal(err)
			}
			var decoded WorkoutSession
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if got := decoded.Exercises[0].Sets[0].RepsInReserve; got == nil || *got != value {
				t.Fatalf("reserve did not round trip: %v", got)
			}
		})
	}
}

func TestCalendarDates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, week, date, zone string
		valid                  bool
	}{
		{name: "inside week", week: "2026-10-26", date: "2026-11-01", zone: "Europe/Moscow", valid: true},
		{name: "next Monday rejected", week: "2026-10-26", date: "2026-11-02", zone: "Europe/Moscow"},
		{name: "week must begin Monday", week: "2026-10-27", date: "2026-10-28", zone: "Europe/Moscow"},
		{name: "unknown timezone rejected", week: "2026-10-26", date: "2026-10-27", zone: "Not/AZone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, err := validateWeek(tc.week, tc.zone)
			if err == nil {
				err = dateInWeek(tc.date, start)
			}
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestSkippedExercisePreservesValidCompletedAndIncompleteDrafts(t *testing.T) {
	t.Parallel()
	finish := time.Now()
	session := WorkoutSession{ID: "00000000-0000-4000-8000-000000000001", Weekday: 1,
		StartedAt: finish.Add(-time.Hour), FinishedAt: &finish, CurrentExercise: 0,
		Exercises: []SessionExercise{{ID: "00000000-0000-4000-8000-000000000002", IsSkipped: true,
			Exercise: Exercise{ID: "00000000-0000-4000-8000-000000000003", Name: "Жим"},
			Sets: []LoggedSet{
				{ID: "00000000-0000-4000-8000-000000000004", Weight: "62,5", Reps: "10", IsCompleted: true},
				{ID: "00000000-0000-4000-8000-000000000005", Weight: "draft", Reps: ""},
			},
		}},
	}
	if err := validateSession(session, true); err != nil {
		t.Fatal(err)
	}
	session.Exercises[0].Sets[0].Weight = "NaN"
	if err := validateSession(session, true); err == nil {
		t.Fatal("nonfinite completed weight accepted")
	}
}
