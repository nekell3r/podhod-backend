package service

import "testing"

func TestRemovedSetIDsAreBoundedUniqueAndAbsentFromSnapshot(t *testing.T) {
	t.Parallel()
	retainedID := "00000000-0000-4000-8000-00000000000a"
	removedID := "00000000-0000-4000-8000-00000000000b"
	session := WorkoutSession{Exercises: []SessionExercise{{Sets: []LoggedSet{{ID: retainedID}}}}}
	cases := []struct {
		name  string
		ids   []string
		valid bool
	}{
		{name: "legacy", valid: true},
		{name: "offline removal", ids: []string{removedID}, valid: true},
		{name: "invalid identity", ids: []string{"other"}},
		{name: "duplicate case", ids: []string{removedID, "00000000-0000-4000-8000-00000000000B"}},
		{name: "still present", ids: []string{retainedID}},
		{name: "too many", ids: make([]string, 601)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRemovedSetIDs(session, tc.ids); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestExplicitRemovalDoesNotBypassCompletedSetOrEffortProtection(t *testing.T) {
	t.Parallel()
	zero := 0
	setID := "00000000-0000-4000-8000-00000000000a"
	sessionID := "00000000-0000-4000-8000-000000000001"
	previous := WorkoutSession{ID: sessionID, Exercises: []SessionExercise{{Sets: []LoggedSet{{ID: setID, RepsInReserve: &zero}}}}}
	removed := WorkoutSession{ID: sessionID, Exercises: []SessionExercise{}}
	if removed.ValidateSetPreservation(previous, nil) == nil {
		t.Fatal("legacy omission lost effort")
	}
	if removed.ValidateSetPreservation(previous, []string{setID}) != nil {
		t.Fatal("explicit unfinished removal rejected")
	}
	retained := WorkoutSession{ID: sessionID, Exercises: []SessionExercise{{Sets: []LoggedSet{{ID: setID}}}}}
	if retained.ValidateSetPreservation(previous, []string{setID}) == nil {
		t.Fatal("removal intent cleared an effort field on a retained set")
	}
	previous.Exercises[0].Sets[0].IsCompleted = true
	for _, reserve := range []*int{nil, &zero} {
		previous.Exercises[0].Sets[0].RepsInReserve = reserve
		if removed.ValidateSetPreservation(previous, []string{setID}) == nil {
			t.Fatal("explicit intent removed a completed set")
		}
	}
}
