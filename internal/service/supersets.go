package service

import (
	"errors"
	"strings"
)

func invalidStoredStructure(err error) error {
	if err == nil {
		return nil
	}
	var applicationError *Error
	if errors.As(err, &applicationError) && applicationError.Code == "upgrade_required" {
		return err
	}
	return &Error{Code: "invalid_resource", Message: "stored workout structure is invalid; original data is preserved"}
}

func validateTemplateStructure(value Template) error {
	hasMetadata := false
	for _, day := range value.Days {
		hasMetadata = hasMetadata || day.Context != nil || len(day.Supersets) > 0
	}
	if err := validateStructureVersion(value.StructureVersion, hasMetadata); err != nil {
		return err
	}
	for _, day := range value.Days {
		if day.Context != nil && day.Context.Cycle != nil {
			return invalid("repeating template must not contain a materialized cycle reference")
		}
		if err := validatePlannedStructure(day.Context, day.Supersets, day.Exercises); err != nil {
			return err
		}
	}
	return nil
}

func validateWeekStructure(value TrainingWeek) error {
	hasMetadata := false
	for _, workout := range value.Workouts {
		hasMetadata = hasMetadata || workout.Context != nil || len(workout.Supersets) > 0
	}
	if err := validateStructureVersion(value.StructureVersion, hasMetadata); err != nil {
		return err
	}
	for _, workout := range value.Workouts {
		if err := validatePlannedStructure(workout.Context, workout.Supersets, workout.Exercises); err != nil {
			return err
		}
	}
	return nil
}

func validateStructureVersion(version int, hasMetadata bool) error {
	if version > 1 {
		return &Error{Code: "upgrade_required", Message: "update the app to preserve workout structure"}
	}
	if version < 0 || (version == 0 && hasMetadata) {
		return invalid("workout structure metadata requires structureVersion 1")
	}
	return nil
}

func (value *WorkoutPlanContext) validate() error {
	if value == nil {
		return nil
	}
	if value.Variant != "" && value.Variant != "A" && value.Variant != "B" && value.Variant != "C" {
		return invalid("workout variant must be A, B, C or empty")
	}
	switch value.LoadType {
	case "regular", "heavy", "light", "deload":
	default:
		return invalid("unknown planned load type")
	}
	switch value.Difficulty {
	case "unspecified", "easy", "moderate", "hard":
	default:
		return invalid("unknown planned difficulty")
	}
	if value.Cycle != nil {
		if !validUUID(value.Cycle.CycleID) || !validUUID(value.Cycle.WeekID) ||
			value.Cycle.WeekNumber < 1 || value.Cycle.WeekNumber > 12 {
			return invalid("invalid cycle week reference")
		}
	}
	return nil
}

func validatePlannedStructure(context *WorkoutPlanContext, groups []PlannedSuperset, plans []PlannedExercise) error {
	if err := context.validate(); err != nil {
		return err
	}
	if len(groups) > 25 {
		return invalid("a day supports at most twenty-five supersets")
	}
	exercises := map[string]PlannedExercise{}
	for _, plan := range plans {
		exercises[strings.ToLower(plan.ID)] = plan
	}
	groupIDs := map[string]bool{}
	membership := map[string]bool{}
	for _, group := range groups {
		id := strings.ToLower(group.ID)
		if !validUUID(id) || groupIDs[id] || len(group.ExerciseIDs) < 2 || len(group.ExerciseIDs) > 4 {
			return invalid("superset requires a unique UUID and two to four ordered exercises")
		}
		groupIDs[id] = true
		if group.RestSeconds < 0 || group.RestSeconds > 600 {
			return invalid("superset round rest must be between zero and 600 seconds")
		}
		rounds := 0
		for _, exerciseID := range group.ExerciseIDs {
			key := strings.ToLower(exerciseID)
			plan, found := exercises[key]
			if !found || membership[key] {
				return invalid("superset members must belong to the day and occur in only one group")
			}
			membership[key] = true
			if rounds != 0 && plan.SetCount != rounds {
				return invalid("planned superset members must have the same number of rounds")
			}
			rounds = plan.SetCount
		}
	}
	return nil
}

func validateSessionStructure(session WorkoutSession) error {
	hasMetadata := session.Context != nil || len(session.Supersets) > 0
	if err := validateStructureVersion(session.StructureVersion, hasMetadata); err != nil {
		return err
	}
	if err := session.Context.validate(); err != nil {
		return err
	}
	if len(session.Supersets) > 50 {
		return invalid("session supports at most fifty superset records")
	}
	type setLocation struct {
		set      LoggedSet
		exercise string
	}
	sets := map[string]setLocation{}
	exercises := map[string]bool{}
	for _, exercise := range session.Exercises {
		exerciseID := strings.ToLower(exercise.ID)
		exercises[exerciseID] = true
		for _, set := range exercise.Sets {
			sets[strings.ToLower(set.ID)] = setLocation{set: set, exercise: exerciseID}
		}
	}
	groupIDs := map[string]bool{}
	roundIDs := map[string]bool{}
	linkedSets := map[string]bool{}
	openMembership := map[string]bool{}
	for _, group := range session.Supersets {
		id := strings.ToLower(group.ID)
		if !validUUID(id) || groupIDs[id] || len(group.ExerciseIDs) < 2 || len(group.ExerciseIDs) > 4 {
			return invalid("session superset requires a unique UUID and two to four ordered members")
		}
		groupIDs[id] = true
		if group.RestSeconds < 0 || group.RestSeconds > 600 || len(group.Rounds) < 1 || len(group.Rounds) > 600 {
			return invalid("invalid superset rest or round count")
		}
		members := map[string]bool{}
		for _, exerciseID := range group.ExerciseIDs {
			key := strings.ToLower(exerciseID)
			if !exercises[key] || members[key] || (!group.IsClosed && openMembership[key]) {
				return invalid("superset members must belong to the session and one open group")
			}
			members[key] = true
			if !group.IsClosed {
				openMembership[key] = true
			}
		}
		lastOrdinal := 0
		for _, round := range group.Rounds {
			key := strings.ToLower(round.ID)
			if !validUUID(key) || roundIDs[key] || round.Ordinal <= lastOrdinal || round.Ordinal > 600 ||
				len(round.SetIDs) != len(group.ExerciseIDs) {
				return invalid("superset rounds require unique ids, increasing ordinals and one slot per member")
			}
			roundIDs[key] = true
			lastOrdinal = round.Ordinal
			participants := map[string]bool{}
			for slot, setID := range round.SetIDs {
				if setID == nil {
					continue
				}
				setKey := strings.ToLower(*setID)
				location, found := sets[setKey]
				if !validUUID(setKey) || !found || linkedSets[setKey] || participants[location.exercise] {
					return invalid("superset set must exist, occur once and use a distinct exercise in its round")
				}
				if group.IsClosed && !location.set.IsCompleted {
					return invalid("closed superset keeps only completed set links")
				}
				if !location.set.IsCompleted && location.exercise != strings.ToLower(group.ExerciseIDs[slot]) {
					return invalid("unfinished superset set must belong to the current member in its slot")
				}
				linkedSets[setKey] = true
				participants[location.exercise] = true
			}
			if len(participants) == 0 {
				return invalid("empty superset rounds must be removed")
			}
		}
	}
	return nil
}

func (session WorkoutSession) validateStructurePreservation(previous WorkoutSession) error {
	if previous.StructureVersion > session.StructureVersion {
		return &Error{Code: "upgrade_required", Message: "update the app to preserve workout structure"}
	}
	sets := map[string]LoggedSet{}
	type sourceExercise struct {
		cardID     string
		exerciseID string
	}
	previousSources := map[string]sourceExercise{}
	currentSources := map[string]sourceExercise{}
	for _, exercise := range previous.Exercises {
		for _, set := range exercise.Sets {
			sets[strings.ToLower(set.ID)] = set
			previousSources[strings.ToLower(set.ID)] = sourceExercise{
				cardID: strings.ToLower(exercise.ID), exerciseID: strings.ToLower(exercise.Exercise.ID),
			}
		}
	}
	for _, exercise := range session.Exercises {
		for _, set := range exercise.Sets {
			currentSources[strings.ToLower(set.ID)] = sourceExercise{
				cardID: strings.ToLower(exercise.ID), exerciseID: strings.ToLower(exercise.Exercise.ID),
			}
		}
	}
	for _, oldGroup := range previous.Supersets {
		var current *SessionSuperset
		for index := range session.Supersets {
			if strings.EqualFold(oldGroup.ID, session.Supersets[index].ID) {
				current = &session.Supersets[index]
			}
		}
		for _, oldRound := range oldGroup.Rounds {
			for slot, setID := range oldRound.SetIDs {
				if setID == nil || !sets[strings.ToLower(*setID)].IsCompleted {
					continue
				}
				preserved := false
				if current != nil && (!oldGroup.IsClosed || current.IsClosed) {
					for _, round := range current.Rounds {
						if !strings.EqualFold(round.ID, oldRound.ID) || round.Ordinal != oldRound.Ordinal || slot >= len(round.SetIDs) {
							continue
						}
						preserved = round.SetIDs[slot] != nil && strings.EqualFold(*round.SetIDs[slot], *setID)
					}
				}
				key := strings.ToLower(*setID)
				if !preserved || currentSources[key] != previousSources[key] {
					return &Error{
						Code:    "superset_conflict",
						Message: "completed superset links must retain their source exercise, group, round and position",
					}
				}
			}
		}
	}
	return nil
}

func (session *WorkoutSession) closeSupersetsAfterSkip() {
	skipped := map[string]bool{}
	completed := map[string]bool{}
	for _, exercise := range session.Exercises {
		skipped[strings.ToLower(exercise.ID)] = exercise.IsSkipped
		for _, set := range exercise.Sets {
			completed[strings.ToLower(set.ID)] = set.IsCompleted
		}
	}
	retained := []SessionSuperset{}
	for _, group := range session.Supersets {
		live := 0
		for _, id := range group.ExerciseIDs {
			if !skipped[strings.ToLower(id)] {
				live++
			}
		}
		if !group.IsClosed && live < 2 {
			group.IsClosed = true
			rounds := []SupersetRound{}
			for _, round := range group.Rounds {
				slots := make([]*string, len(round.SetIDs))
				hasCompleted := false
				for index, id := range round.SetIDs {
					if id != nil && completed[strings.ToLower(*id)] {
						slots[index] = id
						hasCompleted = true
					}
				}
				if hasCompleted {
					round.SetIDs = slots
					rounds = append(rounds, round)
				}
			}
			group.Rounds = rounds
		}
		if len(group.Rounds) > 0 {
			retained = append(retained, group)
		}
	}
	session.Supersets = retained
}
