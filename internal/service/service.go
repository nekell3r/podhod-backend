package service

import (
	"context"
	"encoding/json"
)

type Resource struct {
	Revision int64
	Payload  json.RawMessage
}

type Commit struct {
	Owner            string
	Key              string
	RequestID        string
	RequestHash      string
	ExpectedRevision int64
}

type Repository interface {
	Get(context.Context, string, string) (Resource, error)
	Update(context.Context, Commit, func(Resource) (Resource, error)) (Resource, error)
	History(context.Context, string) ([]WorkoutSession, error)
	AppendHistory(context.Context, string, HistoryWrite) (WorkoutSession, error)
}

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }
