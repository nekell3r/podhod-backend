package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func invalid(message string) error { return &Error{Code: "invalid_request", Message: message} }

func validUUID(value string) bool { return uuidPattern.MatchString(value) }

func emptyTemplate() Template {
	days := make([]TrainingDay, 0, 7)
	for day := 1; day <= 7; day++ {
		days = append(days, TrainingDay{Weekday: day, Exercises: []PlannedExercise{}})
	}
	return Template{Days: days}
}

func (s *Service) GetTemplate(ctx context.Context, owner string) (Template, error) {
	result := emptyTemplate()
	err := s.read(ctx, owner, "template", &result)
	return result, err
}

func (s *Service) ReplaceTemplate(ctx context.Context, owner string, input TemplateWrite) (Template, error) {
	return change(ctx, s, resourceChange[Template]{
		Owner: owner, Key: "template", RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision, Request: input,
		Initial: emptyTemplate(), Transform: func(value *Template) error {
			if len(input.Days) != 7 {
				return invalid("template must contain seven weekdays")
			}
			seen := map[int]bool{}
			for _, day := range input.Days {
				if day.Weekday < 1 || day.Weekday > 7 || seen[day.Weekday] {
					return invalid("invalid or duplicate weekday")
				}
				seen[day.Weekday] = true
				if err := validatePlans(day.Exercises); err != nil {
					return err
				}
			}
			value.Days = input.Days
			return nil
		},
	})
}

func (s *Service) GetWeek(ctx context.Context, owner, weekStart, timezone string) (TrainingWeek, error) {
	if _, err := validateWeek(weekStart, timezone); err != nil {
		return TrainingWeek{}, err
	}
	result := TrainingWeek{WeekStart: weekStart, Timezone: timezone, Workouts: []ScheduledWorkout{}}
	err := s.read(ctx, owner, "week/"+weekStart, &result)
	return result, err
}

func (s *Service) ReplaceWeek(ctx context.Context, owner, weekStart string, input WeekWrite) (TrainingWeek, error) {
	initial := TrainingWeek{WeekStart: weekStart, Timezone: input.Timezone, Workouts: []ScheduledWorkout{}}
	return change(ctx, s, resourceChange[TrainingWeek]{
		Owner: owner, Key: "week/" + weekStart, RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
		Request: input, Initial: initial, Transform: func(value *TrainingWeek) error {
			start, err := validateWeek(weekStart, input.Timezone)
			if err != nil {
				return err
			}
			dates := map[string]bool{}
			ids := map[string]bool{}
			if len(input.Workouts) > 7 {
				return invalid("a week has at most seven workouts")
			}
			for _, workout := range input.Workouts {
				id := strings.ToLower(workout.ID)
				if !validUUID(id) || ids[id] {
					return invalid("invalid or duplicate workout id")
				}
				ids[id] = true
				if err := dateInWeek(workout.PlannedDate, start); err != nil {
					return err
				}
				if dates[workout.PlannedDate] {
					return &Error{Code: "day_occupied", Message: "one workout per day is supported"}
				}
				dates[workout.PlannedDate] = true
				if len(workout.Exercises) == 0 {
					return invalid("scheduled workout requires exercises")
				}
				if err := validatePlans(workout.Exercises); err != nil {
					return err
				}
			}
			value.WeekStart = weekStart
			value.Timezone = input.Timezone
			value.Workouts = input.Workouts
			return nil
		},
	})
}

func (s *Service) MoveWorkout(
	ctx context.Context,
	owner, weekStart string,
	input MoveWorkoutRequest,
) (TrainingWeek, error) {
	initial := TrainingWeek{WeekStart: weekStart, Timezone: "Europe/Moscow", Workouts: []ScheduledWorkout{}}
	return change(ctx, s, resourceChange[TrainingWeek]{
		Owner: owner, Key: "week/" + weekStart, RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
		Request: input, Initial: initial, Transform: func(value *TrainingWeek) error {
			start, err := validateWeek(weekStart, value.Timezone)
			if err != nil {
				return err
			}
			if err := dateInWeek(input.PlannedDate, start); err != nil {
				return err
			}
			index := -1
			for i, workout := range value.Workouts {
				if strings.EqualFold(workout.ID, input.WorkoutID) {
					index = i
					continue
				}
				if workout.PlannedDate == input.PlannedDate {
					return &Error{Code: "day_occupied", Message: "destination already has a workout"}
				}
			}
			if index < 0 {
				return &Error{Code: "not_found", Message: "workout not found"}
			}
			value.Workouts[index].PlannedDate = input.PlannedDate
			return nil
		},
	})
}

func (s *Service) SetScheduledRest(
	ctx context.Context,
	owner, weekStart string,
	input ScheduledRestRequest,
) (TrainingWeek, error) {
	initial := TrainingWeek{WeekStart: weekStart, Timezone: "Europe/Moscow", Workouts: []ScheduledWorkout{}}
	return change(ctx, s, resourceChange[TrainingWeek]{
		Owner: owner, Key: "week/" + weekStart, RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
		Request: input, Initial: initial, Transform: func(value *TrainingWeek) error {
			if err := validateRest(input.RestSeconds, input.RestAfterExerciseSeconds); err != nil {
				return err
			}
			for w := range value.Workouts {
				if !strings.EqualFold(value.Workouts[w].ID, input.WorkoutID) {
					continue
				}
				for e := range value.Workouts[w].Exercises {
					exercise := &value.Workouts[w].Exercises[e]
					if strings.EqualFold(exercise.ID, input.ExerciseID) {
						exercise.RestSeconds = input.RestSeconds
						exercise.RestAfterExerciseSeconds = input.RestAfterExerciseSeconds
						return nil
					}
				}
			}
			return &Error{Code: "not_found", Message: "scheduled exercise not found"}
		},
	})
}

func validateWeek(weekStart, timezone string) (time.Time, error) {
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil || start.Weekday() != time.Monday {
		return time.Time{}, invalid("weekStart must be a Monday in YYYY-MM-DD format")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return time.Time{}, invalid("invalid IANA timezone")
	}
	return start, nil
}

func dateInWeek(value string, start time.Time) error {
	date, err := time.Parse("2006-01-02", value)
	if err != nil || date.Before(start) || !date.Before(start.AddDate(0, 0, 7)) {
		return invalid("date must be inside the selected week")
	}
	return nil
}

func validatePlans(plans []PlannedExercise) error {
	if plans == nil || len(plans) > 50 {
		return invalid("exercise list must contain between zero and fifty entries")
	}
	ids := map[string]bool{}
	for _, plan := range plans {
		id := strings.ToLower(plan.ID)
		if !validUUID(id) || ids[id] {
			return invalid("invalid or duplicate planned exercise id")
		}
		ids[id] = true
		if !validUUID(plan.Exercise.ID) || strings.TrimSpace(plan.Exercise.Name) == "" {
			return invalid("exercise id and name are required")
		}
		if plan.SetCount < 1 || plan.SetCount > 12 || strings.TrimSpace(plan.TargetReps) == "" {
			return invalid("invalid set count or target reps")
		}
		if err := validateRest(plan.RestSeconds, plan.RestAfterExerciseSeconds); err != nil {
			return err
		}
	}
	return nil
}

func validateRest(betweenSets, betweenExercises int) error {
	if betweenSets < 0 || betweenSets > 600 || betweenExercises < 0 || betweenExercises > 600 {
		return invalid("rest must be between zero and 600 seconds")
	}
	return nil
}

func (s *Service) read(ctx context.Context, owner, key string, destination any) error {
	if strings.TrimSpace(owner) == "" {
		return &Error{Code: "unauthorized", Message: "authenticated owner is required"}
	}
	resource, err := s.repository.Get(ctx, owner, key)
	if err != nil {
		return err
	}
	if len(resource.Payload) > 0 {
		if err := json.Unmarshal(resource.Payload, destination); err != nil {
			return fmt.Errorf("decode stored resource: %w", err)
		}
	}
	return nil
}

type resourceChange[T any] struct {
	Owner            string
	Key              string
	RequestID        string
	ExpectedRevision int64
	Request          any
	Initial          T
	Transform        func(*T) error
}

func change[T any](ctx context.Context, s *Service, input resourceChange[T]) (T, error) {
	value := input.Initial
	if strings.TrimSpace(input.Owner) == "" {
		return value, &Error{Code: "unauthorized", Message: "authenticated owner is required"}
	}
	if !validUUID(input.RequestID) || input.ExpectedRevision < 0 {
		return value, invalid("requestID must be a UUID and expectedRevision nonnegative")
	}
	request, err := json.Marshal(struct {
		Key  string
		Body any
	}{Key: input.Key, Body: input.Request})
	if err != nil {
		return value, fmt.Errorf("encode change request: %w", err)
	}
	hash := sha256.Sum256(request)
	commit := Commit{Owner: input.Owner, Key: input.Key, RequestID: strings.ToLower(input.RequestID),
		RequestHash: hex.EncodeToString(hash[:]), ExpectedRevision: input.ExpectedRevision}
	resource, err := s.repository.Update(ctx, commit, func(current Resource) (Resource, error) {
		if len(current.Payload) > 0 {
			if err := json.Unmarshal(current.Payload, &value); err != nil {
				return Resource{}, fmt.Errorf("decode change resource: %w", err)
			}
		}
		if err := input.Transform(&value); err != nil {
			return Resource{}, err
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return Resource{}, fmt.Errorf("encode changed resource: %w", err)
		}
		object := map[string]json.RawMessage{}
		if err := json.Unmarshal(encoded, &object); err != nil {
			return Resource{}, err
		}
		object["revision"] = json.RawMessage(fmt.Sprint(current.Revision + 1))
		encoded, err = json.Marshal(object)
		if err != nil {
			return Resource{}, err
		}
		return Resource{Revision: current.Revision + 1, Payload: encoded}, nil
	})
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(resource.Payload, &value); err != nil {
		return value, fmt.Errorf("decode change result: %w", err)
	}
	return value, nil
}
