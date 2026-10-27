package service

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

func (s *Service) GetActive(ctx context.Context, owner string) (ActiveRecord, error) {
	result := ActiveRecord{}
	err := s.read(ctx, owner, "active", &result)
	return result, err
}

func (s *Service) ReplaceActive(ctx context.Context, owner string, input ActiveWrite) (ActiveRecord, error) {
	return change(ctx, s, resourceChange[ActiveRecord]{
		Owner: owner, Key: "active", RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
		Request: input, Initial: ActiveRecord{}, Transform: func(value *ActiveRecord) error {
			if value.State != nil && input.State != nil {
				sameSession := strings.EqualFold(value.State.Session.ID, input.State.Session.ID)
				dropsRecordedTiming := value.State.Session.Timing != nil && input.State.Session.Timing == nil
				if sameSession && dropsRecordedTiming {
					return &Error{Code: "upgrade_required", Message: "update the app to preserve workout timing"}
				}
			}
			if input.State != nil {
				if err := validateSession(input.State.Session, false); err != nil {
					return err
				}
				validRestKind := input.State.RestKind == nil || *input.State.RestKind == "betweenSets" ||
					*input.State.RestKind == "betweenExercises"
				if !validRestKind {
					return invalid("invalid rest kind")
				}
				if input.State.RestExerciseID != nil {
					found := false
					for _, exercise := range input.State.Session.Exercises {
						if strings.EqualFold(exercise.ID, *input.State.RestExerciseID) {
							found = true
						}
					}
					if !found {
						return invalid("rest exercise must belong to the active session")
					}
				}
			}
			value.State = input.State
			return nil
		},
	})
}

func (s *Service) SetActiveRest(ctx context.Context, owner string, input ActiveRestRequest) (ActiveRecord, error) {
	return change(ctx, s, resourceChange[ActiveRecord]{
		Owner: owner, Key: "active", RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
		Request: input, Initial: ActiveRecord{}, Transform: func(value *ActiveRecord) error {
			if value.State == nil || !strings.EqualFold(value.State.Session.ID, input.SessionID) {
				return &Error{Code: "not_found", Message: "active session not found"}
			}
			if err := validateRest(input.RestSeconds, input.RestAfterExerciseSeconds); err != nil {
				return err
			}
			for i := range value.State.Session.Exercises {
				exercise := &value.State.Session.Exercises[i]
				if !strings.EqualFold(exercise.ID, input.ExerciseID) {
					continue
				}
				exercise.RestSeconds = input.RestSeconds
				exercise.RestAfterExerciseSeconds = input.RestAfterExerciseSeconds
				return nil
			}
			return &Error{Code: "not_found", Message: "active exercise not found"}
		},
	})
}

func (s *Service) SkipExercise(ctx context.Context, owner string, input SkipExerciseRequest) (ActiveRecord, error) {
	return change(ctx, s, resourceChange[ActiveRecord]{
		Owner: owner, Key: "active", RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
		Request: input, Initial: ActiveRecord{}, Transform: func(value *ActiveRecord) error {
			if value.State == nil || !strings.EqualFold(value.State.Session.ID, input.SessionID) {
				return &Error{Code: "not_found", Message: "active session not found"}
			}
			session := &value.State.Session
			index := -1
			for i := range session.Exercises {
				if strings.EqualFold(session.Exercises[i].ID, input.ExerciseID) {
					index = i
				}
			}
			if index < 0 {
				return &Error{Code: "not_found", Message: "active exercise not found"}
			}
			if session.Exercises[index].IsSkipped == input.IsSkipped {
				return nil
			}
			completed := true
			for _, set := range session.Exercises[index].Sets {
				if !set.IsCompleted {
					completed = false
				}
			}
			if input.IsSkipped && completed {
				return invalid("completed exercise cannot be skipped")
			}
			session.Exercises[index].IsSkipped = input.IsSkipped
			if !input.IsSkipped {
				session.CurrentExercise = index
			}
			if input.IsSkipped && session.CurrentExercise == index {
				for offset := 1; offset < len(session.Exercises); offset++ {
					next := (index + offset) % len(session.Exercises)
					candidate := session.Exercises[next]
					pending := !candidate.IsSkipped
					hasUnfinished := false
					for _, set := range candidate.Sets {
						if !set.IsCompleted {
							hasUnfinished = true
						}
					}
					if pending && hasUnfinished {
						session.CurrentExercise = next
						break
					}
				}
			}
			timerForExercise := value.State.RestExerciseID == nil ||
				strings.EqualFold(*value.State.RestExerciseID, input.ExerciseID)
			if input.IsSkipped && timerForExercise {
				value.State.RestUntil = nil
				value.State.RestKind = nil
				value.State.RestExerciseID = nil
			}
			return nil
		},
	})
}

func (s *Service) ListHistory(ctx context.Context, owner string) ([]WorkoutSession, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, &Error{Code: "unauthorized", Message: "authenticated owner is required"}
	}
	return s.repository.History(ctx, owner)
}

func (s *Service) AppendHistory(ctx context.Context, owner string, input HistoryWrite) (WorkoutSession, error) {
	if strings.TrimSpace(owner) == "" {
		return WorkoutSession{}, &Error{Code: "unauthorized", Message: "authenticated owner is required"}
	}
	if !validUUID(input.RequestID) {
		return WorkoutSession{}, invalid("requestID must be a UUID")
	}
	if err := validateSession(input.Session, true); err != nil {
		return WorkoutSession{}, err
	}
	return s.repository.AppendHistory(ctx, owner, input)
}

func validateSession(session WorkoutSession, finished bool) error {
	if !validUUID(session.ID) || session.Weekday < 1 || session.Weekday > 7 {
		return invalid("invalid session identity or weekday")
	}
	if session.StartedAt.IsZero() {
		return invalid("startedAt is required")
	}
	if finished && session.FinishedAt == nil {
		return invalid("history requires finishedAt")
	}
	if !finished && session.FinishedAt != nil {
		return invalid("active session must not be finished")
	}
	if session.FinishedAt != nil && session.FinishedAt.Before(session.StartedAt) {
		return invalid("finishedAt precedes startedAt")
	}
	if len(session.Exercises) == 0 || len(session.Exercises) > 50 {
		return invalid("session requires between one and fifty exercises")
	}
	if session.CurrentExercise < 0 || session.CurrentExercise >= len(session.Exercises) {
		return invalid("current exercise index is invalid")
	}
	if session.ScheduledWorkoutID != nil && !validUUID(*session.ScheduledWorkoutID) {
		return invalid("invalid scheduled workout id")
	}
	if session.PlannedDate != nil {
		if _, err := time.Parse("2006-01-02", *session.PlannedDate); err != nil {
			return invalid("invalid planned date")
		}
	}
	exerciseIDs := map[string]bool{}
	setIDs := map[string]bool{}
	completed := 0
	for _, exercise := range session.Exercises {
		id := strings.ToLower(exercise.ID)
		if !validUUID(id) || exerciseIDs[id] {
			return invalid("invalid or duplicate session exercise id")
		}
		exerciseIDs[id] = true
		if !validUUID(exercise.Exercise.ID) || strings.TrimSpace(exercise.Exercise.Name) == "" {
			return invalid("invalid exercise identity")
		}
		if err := validateRest(exercise.RestSeconds, exercise.RestAfterExerciseSeconds); err != nil {
			return err
		}
		if len(exercise.Sets) < 1 || len(exercise.Sets) > 12 {
			return invalid("exercise requires between one and twelve sets")
		}
		for _, set := range exercise.Sets {
			setID := strings.ToLower(set.ID)
			if !validUUID(setID) || setIDs[setID] {
				return invalid("invalid or duplicate set id")
			}
			setIDs[setID] = true
			if len(set.Reps) > 100 || len(set.Weight) > 100 {
				return invalid("set drafts are too long")
			}
			if !set.IsCompleted {
				continue
			}
			reps, err := strconv.Atoi(strings.TrimSpace(set.Reps))
			if err != nil || reps < 1 || reps > 9999 {
				return invalid("completed set repetitions are invalid")
			}
			weight, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(set.Weight), ",", "."), 64)
			if err != nil || math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 || weight > 2000 {
				return invalid("completed set weight is invalid")
			}
			completed++
		}
	}
	if err := validateWorkoutTiming(session); err != nil {
		return err
	}
	if finished && completed == 0 {
		return invalid("history requires at least one completed set")
	}
	return nil
}
