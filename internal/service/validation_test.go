package service

import (
	"testing"
	"time"
)

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
