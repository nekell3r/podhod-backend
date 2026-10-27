package service

import "strings"

func validateWorkoutTiming(session WorkoutSession) error {
	timing := session.Timing
	if timing == nil {
		return nil
	}
	if timing.Version != 1 {
		return invalid("unsupported workout timing version")
	}
	if timing.TrackedFrom.IsZero() || timing.TrackedFrom.Before(session.StartedAt) {
		return invalid("timing trackedFrom must not precede the session")
	}
	if session.FinishedAt != nil && timing.TrackedFrom.After(*session.FinishedAt) {
		return invalid("timing trackedFrom must not follow the session finish")
	}
	if timing.Intervals == nil {
		return invalid("timing intervals must be an array")
	}
	exercises := make(map[string]bool, len(session.Exercises))
	for _, exercise := range session.Exercises {
		exercises[strings.ToLower(exercise.ID)] = true
	}
	ids := make(map[string]bool, len(timing.Intervals))
	boundary := timing.TrackedFrom
	for index, interval := range timing.Intervals {
		id := strings.ToLower(interval.ID)
		if !validUUID(id) || ids[id] {
			return invalid("invalid or duplicate timing interval id")
		}
		ids[id] = true
		if interval.StartedAt.IsZero() || interval.StartedAt.Before(boundary) {
			return invalid("timing intervals must be ordered and must not overlap")
		}
		if session.FinishedAt != nil && interval.StartedAt.After(*session.FinishedAt) {
			return invalid("timing interval starts after the session finish")
		}
		needsExercise := interval.Kind == "exercise" || interval.Kind == "rest"
		if needsExercise {
			if interval.ExerciseID == nil || !exercises[strings.ToLower(*interval.ExerciseID)] {
				return invalid("timing interval exercise must belong to the session")
			}
		} else if interval.ExerciseID != nil {
			return invalid("other timing interval must not reference an exercise")
		}
		switch interval.Kind {
		case "exercise", "other":
			if interval.RestKind != nil || interval.RestDeadline != nil {
				return invalid("only rest timing intervals may contain rest fields")
			}
		case "rest":
			if interval.RestKind == nil || interval.RestDeadline == nil {
				return invalid("rest timing interval requires its kind and captured deadline")
			}
			if *interval.RestKind != "betweenSets" && *interval.RestKind != "betweenExercises" {
				return invalid("invalid timing rest kind")
			}
			if interval.RestDeadline.Before(interval.StartedAt) {
				return invalid("rest deadline must not precede rest start")
			}
		default:
			return invalid("invalid timing interval kind")
		}
		if interval.EndedAt == nil {
			if session.FinishedAt != nil || index != len(timing.Intervals)-1 {
				return invalid("only the last interval of an active workout may remain open")
			}
			continue
		}
		if interval.EndedAt.Before(interval.StartedAt) {
			return invalid("timing interval ends before it starts")
		}
		if session.FinishedAt != nil && interval.EndedAt.After(*session.FinishedAt) {
			return invalid("timing interval ends after the session finish")
		}
		if interval.Kind == "rest" && interval.EndedAt.After(*interval.RestDeadline) {
			return invalid("recorded rest must not exceed its captured deadline")
		}
		boundary = *interval.EndedAt
	}
	return nil
}
