package service

import (
	"context"
	"errors"
	"strings"
)

type CycleWeekReference struct {
	CycleID    string `json:"cycleID"`
	WeekID     string `json:"weekID"`
	WeekNumber int    `json:"weekNumber"`
}

type WorkoutPlanContext struct {
	Variant    string              `json:"variant"`
	LoadType   string              `json:"loadType"`
	Difficulty string              `json:"difficulty"`
	Cycle      *CycleWeekReference `json:"cycle,omitempty"`
}

type PlannedSuperset struct {
	ID          string   `json:"id"`
	ExerciseIDs []string `json:"exerciseIDs"`
	RestSeconds int      `json:"restSeconds"`
}

type SupersetRound struct {
	ID      string    `json:"id"`
	Ordinal int       `json:"ordinal"`
	SetIDs  []*string `json:"setIDs"`
}

type SessionSuperset struct {
	ID          string          `json:"id"`
	ExerciseIDs []string        `json:"exerciseIDs"`
	RestSeconds int             `json:"restSeconds"`
	IsClosed    bool            `json:"isClosed"`
	Rounds      []SupersetRound `json:"rounds"`
}

type TrainingCycleWeek struct {
	ID   string        `json:"id"`
	Days []TrainingDay `json:"days"`
}

type TrainingCyclePlan struct {
	ID        string              `json:"id"`
	StartWeek string              `json:"startWeek"`
	Timezone  string              `json:"timezone"`
	Repeats   bool                `json:"repeats"`
	Weeks     []TrainingCycleWeek `json:"weeks"`
}

type TrainingCycleDocument struct {
	Version int                `json:"version"`
	Plan    *TrainingCyclePlan `json:"plan"`
}

type TrainingCycleRecord struct {
	Revision int64                 `json:"revision"`
	Document TrainingCycleDocument `json:"document"`
}

type TrainingCycleWrite struct {
	ExpectedRevision int64                 `json:"expectedRevision"`
	RequestID        string                `json:"requestID"`
	Document         TrainingCycleDocument `json:"document"`
}

func (s *Service) GetTrainingCycle(ctx context.Context, owner string) (TrainingCycleRecord, error) {
	record := TrainingCycleRecord{Document: TrainingCycleDocument{Version: 1}}
	if err := s.read(ctx, owner, "cycle", &record); err != nil {
		return TrainingCycleRecord{}, err
	}
	return record, invalidStoredCycle(record.Document.validate())
}

func (s *Service) ReplaceTrainingCycle(
	ctx context.Context,
	owner string,
	input TrainingCycleWrite,
) (TrainingCycleRecord, error) {
	return change(ctx, s, resourceChange[TrainingCycleRecord]{
		Owner: owner, Key: "cycle", RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
		Request: input, Initial: TrainingCycleRecord{Document: TrainingCycleDocument{Version: 1}},
		Transform: func(value *TrainingCycleRecord) error {
			if err := invalidStoredCycle(value.Document.validate()); err != nil {
				return err
			}
			if err := input.Document.validate(); err != nil {
				return err
			}
			value.Document = input.Document
			return nil
		},
	})
}

func (document TrainingCycleDocument) validate() error {
	if document.Version > 1 {
		return &Error{Code: "upgrade_required", Message: "update the app to preserve the training cycle format"}
	}
	if document.Version != 1 {
		return invalid("cycle document version must be 1")
	}
	if document.Plan == nil {
		return nil
	}
	plan := document.Plan
	if !validUUID(plan.ID) || len(plan.Weeks) < 2 || len(plan.Weeks) > 12 {
		return invalid("cycle requires a UUID and between two and twelve weeks")
	}
	if plan.Timezone == "" || plan.Timezone == "Local" {
		return invalid("cycle requires an explicit timezone")
	}
	start, err := validateWeek(plan.StartWeek, plan.Timezone)
	if err != nil {
		return err
	}
	if start.Year() < 1900 || start.AddDate(0, 0, len(plan.Weeks)*7-1).Year() > 9999 {
		return invalid("cycle dates must remain between 1900 and 9999")
	}
	weekIDs := map[string]bool{}
	for _, week := range plan.Weeks {
		id := strings.ToLower(week.ID)
		if !validUUID(id) || weekIDs[id] {
			return invalid("cycle week ids must be unique UUIDs")
		}
		weekIDs[id] = true
		if len(week.Days) != 7 {
			return invalid("each cycle week must contain seven weekdays")
		}
		days := map[int]bool{}
		for _, day := range week.Days {
			if day.Weekday < 1 || day.Weekday > 7 || days[day.Weekday] {
				return invalid("invalid or duplicate cycle weekday")
			}
			days[day.Weekday] = true
			if err := validatePlans(day.Exercises); err != nil {
				return err
			}
			if day.Context != nil && day.Context.Cycle != nil {
				return invalid("cycle templates must not contain a materialized cycle reference")
			}
			if err := validatePlannedStructure(day.Context, day.Supersets, day.Exercises); err != nil {
				return err
			}
		}
	}
	return nil
}

func invalidStoredCycle(err error) error {
	if err == nil {
		return nil
	}
	var applicationError *Error
	if errors.As(err, &applicationError) && applicationError.Code == "upgrade_required" {
		return err
	}
	return &Error{Code: "invalid_resource", Message: "stored training cycle is invalid; original data is preserved"}
}
