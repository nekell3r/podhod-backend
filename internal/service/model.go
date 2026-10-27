package service

import "time"

type Exercise struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Muscle       string `json:"muscle"`
	Illustration string `json:"illustration"`
	Cue          string `json:"cue"`
}

type PlannedExercise struct {
	ID                       string   `json:"id"`
	Exercise                 Exercise `json:"exercise"`
	SetCount                 int      `json:"setCount"`
	TargetReps               string   `json:"targetReps"`
	RestSeconds              int      `json:"restSeconds"`
	RestAfterExerciseSeconds int      `json:"restAfterExerciseSeconds"`
}

type TrainingDay struct {
	Weekday   int               `json:"weekday"`
	Title     string            `json:"title"`
	Exercises []PlannedExercise `json:"exercises"`
}

type Template struct {
	Revision int64         `json:"revision"`
	Days     []TrainingDay `json:"days"`
}

type ScheduledWorkout struct {
	ID          string            `json:"id"`
	PlannedDate string            `json:"plannedDate"`
	Title       string            `json:"title"`
	Exercises   []PlannedExercise `json:"exercises"`
}

type TrainingWeek struct {
	WeekStart string             `json:"weekStart"`
	Timezone  string             `json:"timezone"`
	Revision  int64              `json:"revision"`
	Workouts  []ScheduledWorkout `json:"workouts"`
}

type LoggedSet struct {
	ID          string `json:"id"`
	Reps        string `json:"reps"`
	Weight      string `json:"weight"`
	IsCompleted bool   `json:"isCompleted"`
}

type SessionExercise struct {
	ID                       string      `json:"id"`
	Exercise                 Exercise    `json:"exercise"`
	TargetReps               string      `json:"targetReps"`
	RestSeconds              int         `json:"restSeconds"`
	RestAfterExerciseSeconds int         `json:"restAfterExerciseSeconds"`
	Sets                     []LoggedSet `json:"sets"`
	IsSkipped                bool        `json:"isSkipped"`
}

type WorkoutSession struct {
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	Weekday            int               `json:"weekday"`
	StartedAt          time.Time         `json:"startedAt"`
	FinishedAt         *time.Time        `json:"finishedAt,omitempty"`
	ScheduledWorkoutID *string           `json:"scheduledWorkoutID,omitempty"`
	PlannedDate        *string           `json:"plannedDate,omitempty"`
	CurrentExercise    int               `json:"currentExercise"`
	Exercises          []SessionExercise `json:"exercises"`
	Timing             *WorkoutTiming    `json:"timing,omitempty"`
}

type SessionState struct {
	Session        WorkoutSession `json:"session"`
	RestUntil      *time.Time     `json:"restUntil,omitempty"`
	RestKind       *string        `json:"restKind,omitempty"`
	RestExerciseID *string        `json:"restExerciseID,omitempty"`
}

type ActiveRecord struct {
	Revision int64         `json:"revision"`
	State    *SessionState `json:"state"`
}

type TemplateWrite struct {
	ExpectedRevision int64         `json:"expectedRevision"`
	RequestID        string        `json:"requestID"`
	Days             []TrainingDay `json:"days"`
}

type WeekWrite struct {
	ExpectedRevision int64              `json:"expectedRevision"`
	RequestID        string             `json:"requestID"`
	Timezone         string             `json:"timezone"`
	Workouts         []ScheduledWorkout `json:"workouts"`
}

type MoveWorkoutRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	RequestID        string `json:"requestID"`
	WorkoutID        string `json:"workoutID"`
	PlannedDate      string `json:"plannedDate"`
}

type ScheduledRestRequest struct {
	ExpectedRevision         int64  `json:"expectedRevision"`
	RequestID                string `json:"requestID"`
	WorkoutID                string `json:"workoutID"`
	ExerciseID               string `json:"exerciseID"`
	RestSeconds              int    `json:"restSeconds"`
	RestAfterExerciseSeconds int    `json:"restAfterExerciseSeconds"`
}

type ActiveWrite struct {
	ExpectedRevision int64         `json:"expectedRevision"`
	RequestID        string        `json:"requestID"`
	State            *SessionState `json:"state"`
}

type ActiveRestRequest struct {
	SessionID                string `json:"sessionID"`
	ExpectedRevision         int64  `json:"expectedRevision"`
	RequestID                string `json:"requestID"`
	ExerciseID               string `json:"exerciseID"`
	RestSeconds              int    `json:"restSeconds"`
	RestAfterExerciseSeconds int    `json:"restAfterExerciseSeconds"`
}

type SkipExerciseRequest struct {
	SessionID        string `json:"sessionID"`
	ExpectedRevision int64  `json:"expectedRevision"`
	RequestID        string `json:"requestID"`
	ExerciseID       string `json:"exerciseID"`
	IsSkipped        bool   `json:"isSkipped"`
}

type HistoryWrite struct {
	RequestID string         `json:"requestID"`
	Session   WorkoutSession `json:"session"`
}

type Error struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	CurrentRevision int64  `json:"currentRevision"`
}

func (e *Error) Error() string { return e.Message }

type ProteinRecord struct {
	Date           string     `json:"date"`
	Revision       int64      `json:"revision"`
	MorningTakenAt *time.Time `json:"morningTakenAt,omitempty"`
	EveningTakenAt *time.Time `json:"eveningTakenAt,omitempty"`
}

type ProteinWrite struct {
	ExpectedRevision int64      `json:"expectedRevision"`
	RequestID        string     `json:"requestID"`
	MorningTakenAt   *time.Time `json:"morningTakenAt,omitempty"`
	EveningTakenAt   *time.Time `json:"eveningTakenAt,omitempty"`
}

type WorkoutTiming struct {
	Version     int                     `json:"version"`
	TrackedFrom time.Time               `json:"trackedFrom"`
	Intervals   []WorkoutTimingInterval `json:"intervals"`
}

type WorkoutTimingInterval struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	ExerciseID   *string    `json:"exerciseID,omitempty"`
	RestKind     *string    `json:"restKind,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	EndedAt      *time.Time `json:"endedAt,omitempty"`
	RestDeadline *time.Time `json:"restDeadline,omitempty"`
}
