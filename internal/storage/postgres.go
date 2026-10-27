package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"podhod/server/internal/service"
)

type Postgres struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
 CREATE TABLE IF NOT EXISTS training_resources (
  owner TEXT NOT NULL, resource_key TEXT NOT NULL, revision BIGINT NOT NULL CHECK(revision>0),
  payload JSONB NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(owner,resource_key)
 );
 CREATE TABLE IF NOT EXISTS training_requests (
  owner TEXT NOT NULL, request_id TEXT NOT NULL, request_hash TEXT NOT NULL,
  result JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(owner,request_id)
 );
 CREATE TABLE IF NOT EXISTS workout_history (
  owner TEXT NOT NULL, session_id TEXT NOT NULL, payload JSONB NOT NULL,
  finished_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(owner,session_id)
 );
 CREATE TABLE IF NOT EXISTS training_audit (
  id BIGSERIAL PRIMARY KEY, owner TEXT NOT NULL, resource_key TEXT NOT NULL,
  request_id TEXT NOT NULL, revision BIGINT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
 );`)
	if err != nil {
		return fmt.Errorf("migrate training tables: %w", err)
	}
	return nil
}

func (p *Postgres) Get(ctx context.Context, owner, key string) (service.Resource, error) {
	resource := service.Resource{}
	err := p.pool.QueryRow(
		ctx,
		"SELECT revision,payload FROM training_resources WHERE owner=$1 AND resource_key=$2",
		owner,
		key,
	).
		Scan(&resource.Revision, &resource.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return resource, nil
	}
	if err != nil {
		return resource, fmt.Errorf("read training resource: %w", err)
	}
	return resource, nil
}

func (p *Postgres) Update(
	ctx context.Context,
	input service.Commit,
	transform func(service.Resource) (service.Resource, error),
) (service.Resource, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return service.Resource{}, fmt.Errorf("begin training update: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", input.Owner); err != nil {
		return service.Resource{}, fmt.Errorf("lock owner changes: %w", err)
	}
	var storedHash string
	var encoded []byte
	err = tx.QueryRow(
		ctx,
		"SELECT request_hash,result FROM training_requests WHERE owner=$1 AND request_id=$2",
		input.Owner,
		input.RequestID,
	).
		Scan(&storedHash, &encoded)
	if err == nil {
		if storedHash != input.RequestHash {
			return service.Resource{}, &service.Error{
				Code:    "idempotency_conflict",
				Message: "requestID was already used for another command",
			}
		}
		var result service.Resource
		if err := json.Unmarshal(encoded, &result); err != nil {
			return result, fmt.Errorf("decode prior request result: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return service.Resource{}, fmt.Errorf("read prior request: %w", err)
	}
	current := service.Resource{}
	err = tx.QueryRow(
		ctx,
		"SELECT revision,payload FROM training_resources WHERE owner=$1 AND resource_key=$2",
		input.Owner,
		input.Key,
	).
		Scan(&current.Revision, &current.Payload)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return current, fmt.Errorf("read resource for update: %w", err)
	}
	if current.Revision != input.ExpectedRevision {
		return current, &service.Error{
			Code:            "revision_conflict",
			Message:         "resource changed; fetch and reconcile before retrying",
			CurrentRevision: current.Revision,
		}
	}
	result, err := transform(current)
	if err != nil {
		return current, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO training_resources(owner,resource_key,revision,payload) VALUES($1,$2,$3,$4)
  ON CONFLICT(owner,resource_key) DO UPDATE SET revision=EXCLUDED.revision,payload=EXCLUDED.payload,updated_at=now()`,
		input.Owner, input.Key, result.Revision, result.Payload); err != nil {
		return current, fmt.Errorf("write training resource: %w", err)
	}
	encoded, err = json.Marshal(result)
	if err != nil {
		return current, fmt.Errorf("encode idempotency result: %w", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO training_requests(owner,request_id,request_hash,result) VALUES($1,$2,$3,$4)",
		input.Owner, input.RequestID, input.RequestHash, encoded); err != nil {
		return current, fmt.Errorf("write idempotency record: %w", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO training_audit(owner,resource_key,request_id,revision) VALUES($1,$2,$3,$4)",
		input.Owner, input.Key, input.RequestID, result.Revision); err != nil {
		return current, fmt.Errorf("write change audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return current, fmt.Errorf("commit training update: %w", err)
	}
	return result, nil
}

func (p *Postgres) History(ctx context.Context, owner string) ([]service.WorkoutSession, error) {
	rows, err := p.pool.Query(
		ctx,
		"SELECT payload FROM workout_history WHERE owner=$1 ORDER BY finished_at DESC,session_id",
		owner,
	)
	if err != nil {
		return nil, fmt.Errorf("read workout history: %w", err)
	}
	defer rows.Close()
	sessions := []service.WorkoutSession{}
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan workout history: %w", err)
		}
		var session service.WorkoutSession
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, fmt.Errorf("decode workout history: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workout history: %w", err)
	}
	return sessions, nil
}

func (p *Postgres) AppendHistory(
	ctx context.Context,
	owner string,
	input service.HistoryWrite,
) (service.WorkoutSession, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return service.WorkoutSession{}, fmt.Errorf("begin history append: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", owner); err != nil {
		return service.WorkoutSession{}, err
	}
	request, err := json.Marshal(input)
	if err != nil {
		return service.WorkoutSession{}, err
	}
	digest := sha256.Sum256(append([]byte("history/"+strings.ToLower(input.Session.ID)), request...))
	hash := hex.EncodeToString(digest[:])
	var priorHash string
	var priorResult []byte
	err = tx.QueryRow(
		ctx,
		"SELECT request_hash,result FROM training_requests WHERE owner=$1 AND request_id=$2",
		owner,
		strings.ToLower(input.RequestID),
	).Scan(&priorHash, &priorResult)
	if err == nil {
		if priorHash != hash {
			return service.WorkoutSession{}, &service.Error{
				Code:    "idempotency_conflict",
				Message: "requestID was already used for another command",
			}
		}
		var session service.WorkoutSession
		if err := json.Unmarshal(priorResult, &session); err != nil {
			return session, err
		}
		return session, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return service.WorkoutSession{}, err
	}
	encoded, err := json.Marshal(input.Session)
	if err != nil {
		return service.WorkoutSession{}, err
	}
	var existing []byte
	err = tx.QueryRow(
		ctx,
		"SELECT payload FROM workout_history WHERE owner=$1 AND session_id=$2",
		owner,
		strings.ToLower(input.Session.ID),
	).Scan(&existing)
	if err == nil {
		var old service.WorkoutSession
		if err := json.Unmarshal(existing, &old); err != nil {
			return old, err
		}
		canonical, err := json.Marshal(old)
		if err != nil {
			return old, err
		}
		if string(canonical) != string(encoded) {
			return old, &service.Error{Code: "history_immutable", Message: "completed workout history cannot be overwritten"}
		}
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			return service.WorkoutSession{}, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO workout_history(owner,session_id,payload,finished_at) VALUES($1,$2,$3,$4)",
			owner, strings.ToLower(input.Session.ID), encoded, input.Session.FinishedAt); err != nil {
			return service.WorkoutSession{}, err
		}
	}
	if _, err := tx.Exec(ctx, "INSERT INTO training_requests(owner,request_id,request_hash,result) VALUES($1,$2,$3,$4)",
		owner, strings.ToLower(input.RequestID), hash, encoded); err != nil {
		return service.WorkoutSession{}, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO training_audit(owner,resource_key,request_id,revision) VALUES($1,$2,$3,1)",
		owner, "history/"+strings.ToLower(input.Session.ID), strings.ToLower(input.RequestID)); err != nil {
		return service.WorkoutSession{}, fmt.Errorf("audit history append: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return service.WorkoutSession{}, fmt.Errorf("commit history append: %w", err)
	}
	return input.Session, nil
}
