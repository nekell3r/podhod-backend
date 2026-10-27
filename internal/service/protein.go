package service

import (
	"context"
	"time"
)

func (s *Service) GetProtein(ctx context.Context, owner, date string) (ProteinRecord, error) {
	result := ProteinRecord{Date: date}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return result, invalid("date must be YYYY-MM-DD")
	}
	err := s.read(ctx, owner, "protein/"+date, &result)
	return result, err
}

func (s *Service) ReplaceProtein(ctx context.Context, owner, date string, input ProteinWrite) (ProteinRecord, error) {
	return change(ctx, s, resourceChange[ProteinRecord]{
		Owner: owner, Key: "protein/" + date, RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision,
		Request: input, Initial: ProteinRecord{Date: date}, Transform: func(record *ProteinRecord) error {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return invalid("date must be YYYY-MM-DD")
			}
			record.MorningTakenAt = input.MorningTakenAt
			record.EveningTakenAt = input.EveningTakenAt
			return nil
		},
	})
}
