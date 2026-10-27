package mcptransport_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcptransport "podhod/server/internal/mcp"
	"podhod/server/internal/service"
)

const (
	weekStart   = "2026-10-26"
	workoutID   = "10000000-0000-4000-8000-000000000001"
	exerciseID  = "20000000-0000-4000-8000-000000000001"
	sessionID   = "30000000-0000-4000-8000-000000000001"
	metadataURL = "https://podhod.example/.well-known/oauth-protected-resource"
)

type identity struct {
	owner  string
	scopes []string
}

type tokenTransport struct{}

func (tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	authenticated := req.Clone(req.Context())
	authenticated.Header.Set("Authorization", "Bearer test-connection")
	return http.DefaultTransport.RoundTrip(authenticated)
}

func connect(t *testing.T, svc *service.Service, account identity) *sdkmcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if token != "test-connection" {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{
			UserID:     account.owner,
			Scopes:     account.scopes,
			Expiration: time.Now().Add(time.Hour),
		}, nil
	}
	middleware := auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: metadataURL,
	})
	server := httptest.NewServer(middleware(mcptransport.NewHandler(svc, metadataURL)))
	t.Cleanup(server.Close)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "podhod-test"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:   server.URL,
		HTTPClient: &http.Client{Transport: tokenTransport{}, Timeout: 10 * time.Second},
	}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close MCP client: %v", err)
		}
	})
	return session
}

func callTool(t *testing.T, client *sdkmcp.ClientSession, name string, input any) *sdkmcp.CallToolResult {
	t.Helper()
	result, err := client.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

func decodeResult[T any](t *testing.T, result *sdkmcp.CallToolResult) T {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("encode MCP result: %v", err)
	}
	var output T
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatalf("decode MCP result: %v", err)
	}
	return output
}

type failure struct {
	Code            string `json:"code"`
	CurrentRevision *int64 `json:"current_revision"`
}

type weekResponse struct {
	Week  service.TrainingWeek `json:"week"`
	Error *failure             `json:"error"`
}

type activeResponse struct {
	Active         service.ActiveRecord `json:"active"`
	DeliveryStatus string               `json:"delivery_status"`
	Notice         string               `json:"notice"`
	Error          *failure             `json:"error"`
}

func testPlans() []service.PlannedExercise {
	return []service.PlannedExercise{{
		ID: exerciseID,
		Exercise: service.Exercise{
			ID:           "40000000-0000-4000-8000-000000000001",
			Name:         "Жим штанги лёжа",
			Muscle:       "Грудь",
			Illustration: "bench",
			Cue:          "Контролируй движение",
		},
		SetCount:                 2,
		TargetReps:               "8–12",
		RestSeconds:              90,
		RestAfterExerciseSeconds: 120,
	}}
}

func weekWrite(requestID string) map[string]any {
	return map[string]any{
		"week_start":        weekStart,
		"timezone":          "Europe/Moscow",
		"expected_revision": 0,
		"request_id":        requestID,
		"workouts": []service.ScheduledWorkout{{
			ID: workoutID, PlannedDate: "2026-10-26", Title: "Верх тела", Exercises: testPlans(),
		}},
	}
}

func TestToolDiscoveryAndProfile(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	client := connect(t, service.New(repo), identity{owner: "owner-a"})
	tools, err := client.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 14 {
		t.Fatalf("tool count = %d, want 14", len(tools.Tools))
	}
	seen := map[string]bool{}
	for _, tool := range tools.Tools {
		seen[tool.Name] = true
		if tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("tool %s is not declared as bounded to the private account", tool.Name)
		}
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
		if schema["additionalProperties"] != false {
			t.Errorf("tool %s input schema is not closed", tool.Name)
		}
		if tool.Name == "get_profile" {
			if tool.Meta["openai/profile"] != true || !tool.Annotations.ReadOnlyHint {
				t.Error("profile discovery metadata is missing")
			}
		}
	}
	for _, name := range []string{"skip_active_exercise", "unskip_active_exercise", "update_active_exercise_rest"} {
		if !seen[name] {
			t.Errorf("missing %s", name)
		}
	}
	result := callTool(
		t,
		client,
		"get_profile",
		map[string]any{},
	)
	if result.IsError {
		t.Fatalf("profile failed: %+v", result)
	}
	profile := decodeResult[map[string]any](t, result)
	if !reflect.DeepEqual(profile, map[string]any{"id": "owner-a"}) {
		t.Fatalf("profile = %+v", profile)
	}
	if repo.writeCount() != 0 {
		t.Error("read-only discovery/profile changed data")
	}
}

func TestScopesAndIdentityAreEnforced(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		identity identity
		tool     string
		input    any
	}{
		{
			name:     "missing stable identity",
			identity: identity{scopes: []string{mcptransport.ScopeScheduleRead}},
			tool:     "get_profile", input: map[string]any{},
		},
		{
			name:     "read scope cannot write",
			identity: identity{owner: "owner-a", scopes: []string{mcptransport.ScopeScheduleRead}},
			tool:     "replace_training_week", input: weekWrite("50000000-0000-4000-8000-000000000001"),
		},
		{
			name:     "schedule scope cannot read active sessions",
			identity: identity{owner: "owner-a", scopes: []string{mcptransport.ScopeScheduleRead}},
			tool:     "get_active_workout", input: map[string]any{},
		},
		{
			name:     "workout scope cannot read schedule",
			identity: identity{owner: "owner-a", scopes: []string{mcptransport.ScopeWorkoutRead}},
			tool:     "get_training_week", input: map[string]any{"week_start": weekStart, "timezone": "Europe/Moscow"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newMemoryRepository()
			result := callTool(
				t,
				connect(t, service.New(repo), tc.identity),
				tc.tool,
				tc.input,
			)
			if !result.IsError || result.Meta["mcp/www_authenticate"] == nil {
				t.Fatalf("expected an OAuth challenge, got %+v", result)
			}
			if repo.writeCount() != 0 {
				t.Error("unauthorized tool wrote data")
			}
		})
	}
}

func TestOwnerIsolationAndNoCallerSelectedOwner(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	svc := service.New(repo)
	ownerA := connect(t, svc, identity{
		owner: "owner-a", scopes: []string{mcptransport.ScopeScheduleRead, mcptransport.ScopeScheduleWrite},
	})
	ownerB := connect(t, svc, identity{owner: "owner-b", scopes: []string{mcptransport.ScopeScheduleRead}})
	created := callTool(
		t,
		ownerA,
		"replace_training_week",
		weekWrite("50000000-0000-4000-8000-000000000001"),
	)
	if created.IsError {
		t.Fatalf("create week failed: %+v", created)
	}
	other := callTool(
		t,
		ownerB,
		"get_training_week",
		map[string]any{
			"week_start": weekStart, "timezone": "Europe/Moscow",
		},
	)
	week := decodeResult[weekResponse](t, other).Week
	if other.IsError || week.Revision != 0 || len(week.Workouts) != 0 {
		t.Fatalf("another account's plan leaked: %+v", other)
	}
	injected := callTool(
		t,
		ownerB,
		"get_training_week",
		map[string]any{
			"week_start": weekStart, "timezone": "Europe/Moscow", "owner_id": "owner-a",
		},
	)
	if !injected.IsError {
		t.Fatal("caller-selected owner was not rejected by the schema")
	}
}

func TestScheduleMutationsRevisionsAndIdempotency(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	client := connect(t, service.New(repo), identity{
		owner: "owner-a", scopes: []string{mcptransport.ScopeScheduleRead, mcptransport.ScopeScheduleWrite},
	})
	input := weekWrite("50000000-0000-4000-8000-000000000001")
	first := callTool(
		t,
		client,
		"replace_training_week",
		input,
	)
	if first.IsError || decodeResult[weekResponse](t, first).Week.Revision != 1 {
		t.Fatalf("failed to create week: %+v", first)
	}
	retry := callTool(
		t,
		client,
		"replace_training_week",
		input,
	)
	if retry.IsError || decodeResult[weekResponse](t, retry).Week.Revision != 1 || repo.writeCount() != 1 {
		t.Fatalf("retry duplicated the mutation: %+v", retry)
	}
	stale := weekWrite("50000000-0000-4000-8000-000000000002")
	conflict := callTool(
		t,
		client,
		"replace_training_week",
		stale,
	)
	detail := decodeResult[weekResponse](t, conflict).Error
	if !conflict.IsError || detail == nil || detail.Code != "revision_conflict" {
		t.Fatalf("stale write was not rejected: %+v", conflict)
	}
	if detail.CurrentRevision == nil || *detail.CurrentRevision != 1 {
		t.Fatalf("conflict does not expose current_revision: %+v", detail)
	}
	changedRetry := weekWrite("50000000-0000-4000-8000-000000000001")
	changedRetry["timezone"] = "UTC"
	collision := callTool(
		t,
		client,
		"replace_training_week",
		changedRetry,
	)
	if !collision.IsError || decodeResult[weekResponse](t, collision).Error.Code != "idempotency_conflict" {
		t.Fatalf("request ID reuse was accepted: %+v", collision)
	}
	moved := callTool(
		t,
		client,
		"move_scheduled_workout",
		map[string]any{
			"week_start": weekStart, "workout_id": workoutID, "to_date": "2026-10-28",
			"expected_revision": 1, "request_id": "50000000-0000-4000-8000-000000000003",
		},
	)
	week := decodeResult[weekResponse](t, moved).Week
	if moved.IsError || week.Revision != 2 || week.Workouts[0].PlannedDate != "2026-10-28" {
		t.Fatalf("move failed: %+v", moved)
	}
	rest := callTool(
		t,
		client,
		"update_planned_exercise_rest",
		map[string]any{
			"week_start": weekStart, "workout_id": workoutID, "exercise_id": exerciseID,
			"between_sets_seconds": 75, "after_exercise_seconds": 180,
			"expected_revision": 2, "request_id": "50000000-0000-4000-8000-000000000004",
		},
	)
	week = decodeResult[weekResponse](t, rest).Week
	if rest.IsError || week.Revision != 3 {
		t.Fatalf("rest update failed: %+v", rest)
	}
	plan := week.Workouts[0].Exercises[0]
	if plan.RestSeconds != 75 || plan.RestAfterExerciseSeconds != 180 {
		t.Fatalf("wrong planned rest values: %+v", plan)
	}
}

func TestActiveRestAndSkipPreserveLogsAndReportPendingSync(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	svc := service.New(repo)
	state := activeState()
	seeded, err := svc.ReplaceActive(t.Context(), "owner-a", service.ActiveWrite{
		ExpectedRevision: 0, RequestID: "60000000-0000-4000-8000-000000000001", State: &state,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := connect(t, svc, identity{
		owner: "owner-a", scopes: []string{mcptransport.ScopeWorkoutRead, mcptransport.ScopeWorkoutWrite},
	})
	before := append([]service.LoggedSet{}, seeded.State.Session.Exercises[0].Sets...)
	rest := callTool(
		t,
		client,
		"update_active_exercise_rest",
		map[string]any{
			"session_id": sessionID, "exercise_id": exerciseID,
			"between_sets_seconds": 45, "after_exercise_seconds": 150,
			"expected_revision": 1, "request_id": "60000000-0000-4000-8000-000000000002",
		},
	)
	restResult := decodeResult[activeResponse](t, rest)
	if rest.IsError || restResult.Active.Revision != 2 {
		t.Fatalf("active rest failed: %+v", rest)
	}
	if !restResult.Active.State.RestUntil.Equal(*state.RestUntil) {
		t.Fatal("changing future rest altered the running timer")
	}
	skip := callTool(
		t,
		client,
		"skip_active_exercise",
		map[string]any{
			"session_id": sessionID, "exercise_id": exerciseID,
			"expected_revision": 2, "request_id": "60000000-0000-4000-8000-000000000003",
		},
	)
	result := decodeResult[activeResponse](t, skip)
	if skip.IsError || result.Active.Revision != 3 || result.DeliveryStatus != "pending_sync" {
		t.Fatalf("skip failed or falsely claimed phone acknowledgement: %+v", skip)
	}
	if !strings.Contains(result.Notice, "next successful sync") {
		t.Fatalf("missing offline-delivery notice: %q", result.Notice)
	}
	if !result.Active.State.Session.Exercises[0].IsSkipped {
		t.Fatal("skip flag was not set")
	}
	if !reflect.DeepEqual(result.Active.State.Session.Exercises[0].Sets, before) {
		t.Fatal("skip changed recorded sets")
	}
	if result.Active.State.Session.CurrentExercise != 1 || result.Active.State.RestUntil != nil {
		t.Fatal("skip did not advance past the exercise and clear its rest")
	}
	unskip := callTool(
		t,
		client,
		"unskip_active_exercise",
		map[string]any{
			"session_id": sessionID, "exercise_id": exerciseID,
			"expected_revision": 3, "request_id": "60000000-0000-4000-8000-000000000004",
		},
	)
	unskipped := decodeResult[activeResponse](t, unskip)
	if unskip.IsError || unskipped.Active.State.Session.Exercises[0].IsSkipped {
		t.Fatalf("unskip failed: %+v", unskip)
	}
	wrongSession := callTool(
		t,
		client,
		"skip_active_exercise",
		map[string]any{
			"session_id": "30000000-0000-4000-8000-000000000099", "exercise_id": exerciseID,
			"expected_revision": 4, "request_id": "60000000-0000-4000-8000-000000000005",
		},
	)
	if !wrongSession.IsError || decodeResult[activeResponse](t, wrongSession).Error.Code != "not_found" {
		t.Fatalf("command addressed to another session was accepted: %+v", wrongSession)
	}
}

func activeState() service.SessionState {
	rest := time.Date(
		2026,
		10,
		26,
		20,
		0,
		0,
		0,
		time.UTC,
	)
	kind := "betweenSets"
	id := exerciseID
	plan := testPlans()[0]
	first := service.SessionExercise{
		ID: plan.ID, Exercise: plan.Exercise, TargetReps: "8–12",
		RestSeconds: 90, RestAfterExerciseSeconds: 120,
		Sets: []service.LoggedSet{
			{ID: "70000000-0000-4000-8000-000000000001", Reps: "10", Weight: "60", IsCompleted: true},
			{ID: "70000000-0000-4000-8000-000000000002", Reps: "8", Weight: "55"},
		},
	}
	second := first
	second.ID = "20000000-0000-4000-8000-000000000002"
	second.Sets = []service.LoggedSet{{ID: "70000000-0000-4000-8000-000000000003"}}
	return service.SessionState{
		Session: service.WorkoutSession{
			ID: sessionID, Title: "Верх тела", Weekday: 1,
			StartedAt: time.Date(
				2026,
				10,
				26,
				19,
				0,
				0,
				0,
				time.UTC,
			),
			Exercises: []service.SessionExercise{first, second},
		},
		RestUntil: &rest, RestKind: &kind, RestExerciseID: &id,
	}
}

type resourceKey struct {
	owner string
	key   string
}

type savedRequest struct {
	hash   string
	result service.Resource
}

type memoryRepository struct {
	mu        sync.Mutex
	resources map[resourceKey]service.Resource
	requests  map[resourceKey]savedRequest
	history   map[string][]service.WorkoutSession
	writes    int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		resources: map[resourceKey]service.Resource{},
		requests:  map[resourceKey]savedRequest{},
		history:   map[string][]service.WorkoutSession{},
	}
}

func (r *memoryRepository) Get(_ context.Context, owner, key string) (service.Resource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resources[resourceKey{owner: owner, key: key}], nil
}

func (r *memoryRepository) RequestResult(
	_ context.Context,
	_, _, _ string,
) (json.RawMessage, bool, error) {
	return nil, false, fmt.Errorf("unexpected legacy RequestResult call")
}

func (r *memoryRepository) Update(
	_ context.Context,
	input service.Commit,
	transform func(service.Resource) (service.Resource, error),
) (service.Resource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	requestKey := resourceKey{owner: input.Owner, key: input.RequestID}
	if prior, exists := r.requests[requestKey]; exists {
		if prior.hash != input.RequestHash {
			return service.Resource{}, &service.Error{Code: "idempotency_conflict", Message: "request ID reused"}
		}
		return prior.result, nil
	}
	key := resourceKey{owner: input.Owner, key: input.Key}
	current := r.resources[key]
	if current.Revision != input.ExpectedRevision {
		return current, &service.Error{
			Code: "revision_conflict", Message: "resource changed", CurrentRevision: current.Revision,
		}
	}
	updated, err := transform(current)
	if err != nil {
		return current, err
	}
	r.resources[key] = updated
	r.requests[requestKey] = savedRequest{hash: input.RequestHash, result: updated}
	r.writes++
	return updated, nil
}

func (r *memoryRepository) History(_ context.Context, owner string) ([]service.WorkoutSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]service.WorkoutSession{}, r.history[owner]...), nil
}

func (r *memoryRepository) AppendHistory(
	_ context.Context,
	owner string,
	input service.HistoryWrite,
) (service.WorkoutSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history[owner] = append(r.history[owner], input.Session)
	return input.Session, nil
}

func (r *memoryRepository) writeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writes
}

func TestScheduleValidationAndOccupiedDayDoNotOverwrite(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	client := connect(t, service.New(repo), identity{
		owner: "owner-a", scopes: []string{mcptransport.ScopeScheduleRead, mcptransport.ScopeScheduleWrite},
	})
	input := weekWrite("50000000-0000-4000-8000-000000000011")
	input["workouts"] = []service.ScheduledWorkout{
		{ID: workoutID, PlannedDate: "2026-10-26", Title: "Первая", Exercises: testPlans()},
		{ID: "10000000-0000-4000-8000-000000000002", PlannedDate: "2026-10-28", Exercises: testPlans()},
	}
	created := callTool(
		t,
		client,
		"replace_training_week",
		input,
	)
	if created.IsError {
		t.Fatalf("create week: %+v", created)
	}
	occupied := callTool(
		t,
		client,
		"move_scheduled_workout",
		map[string]any{
			"week_start": weekStart, "workout_id": workoutID, "to_date": "2026-10-28",
			"expected_revision": 1, "request_id": "50000000-0000-4000-8000-000000000012",
		},
	)
	if !occupied.IsError || decodeResult[weekResponse](t, occupied).Error.Code != "day_occupied" {
		t.Fatalf("occupied day was overwritten: %+v", occupied)
	}
	invalidRest := callTool(
		t,
		client,
		"update_planned_exercise_rest",
		map[string]any{
			"week_start": weekStart, "workout_id": workoutID, "exercise_id": exerciseID,
			"between_sets_seconds": -1, "after_exercise_seconds": 30,
			"expected_revision": 1, "request_id": "50000000-0000-4000-8000-000000000013",
		},
	)
	if !invalidRest.IsError || decodeResult[weekResponse](t, invalidRest).Error.Code != "invalid_request" {
		t.Fatalf("negative rest was accepted: %+v", invalidRest)
	}
	latest := callTool(
		t,
		client,
		"get_training_week",
		map[string]any{
			"week_start": weekStart, "timezone": "Europe/Moscow",
		},
	)
	week := decodeResult[weekResponse](t, latest).Week
	if week.Revision != 1 || week.Workouts[0].PlannedDate != "2026-10-26" || len(week.Workouts) != 2 {
		t.Fatalf("rejected mutations changed schedule: %+v", week)
	}
}

func TestTemplateChangesLeaveDatedWeeksAndActiveSnapshotUntouched(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	svc := service.New(repo)
	state := activeState()
	if _, err := svc.ReplaceActive(t.Context(), "owner-a", service.ActiveWrite{
		State: &state, RequestID: "60000000-0000-4000-8000-000000000011",
	}); err != nil {
		t.Fatal(err)
	}
	client := connect(t, svc, identity{
		owner: "owner-a", scopes: []string{mcptransport.ScopeScheduleRead, mcptransport.ScopeScheduleWrite},
	})
	created := callTool(
		t,
		client,
		"replace_training_week",
		weekWrite("50000000-0000-4000-8000-000000000021"),
	)
	if created.IsError {
		t.Fatalf("create week: %+v", created)
	}
	days := make([]service.TrainingDay, 0, 7)
	for i := 1; i <= 7; i++ {
		days = append(days, service.TrainingDay{Weekday: i, Exercises: []service.PlannedExercise{}})
	}
	days[1].Title = "Другая программа"
	days[1].Exercises = testPlans()
	changed := callTool(
		t,
		client,
		"replace_training_template",
		map[string]any{
			"expected_revision": 0, "request_id": "50000000-0000-4000-8000-000000000022", "days": days,
		},
	)
	if changed.IsError {
		t.Fatalf("replace template: %+v", changed)
	}
	week, err := svc.GetWeek(
		t.Context(),
		"owner-a",
		weekStart,
		"Europe/Moscow",
	)
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.GetActive(t.Context(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if week.Revision != 1 || week.Workouts[0].PlannedDate != "2026-10-26" {
		t.Fatal("template replacement changed a dated week")
	}
	if active.Revision != 1 || !reflect.DeepEqual(*active.State, state) {
		t.Fatal("template replacement changed the active workout")
	}
}

func TestHistoryReadIsLimitedAndDoesNotExposeOtherAccounts(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	repo.history["owner-a"] = []service.WorkoutSession{{ID: "first"}, {ID: "second"}, {ID: "third"}}
	repo.history["owner-b"] = []service.WorkoutSession{{ID: "private-other"}}
	client := connect(t, service.New(repo), identity{
		owner: "owner-a", scopes: []string{mcptransport.ScopeWorkoutRead},
	})
	result := callTool(
		t,
		client,
		"get_workout_history",
		map[string]any{"limit": 2},
	)
	output := decodeResult[struct {
		Sessions []service.WorkoutSession `json:"sessions"`
	}](t, result)
	if result.IsError || len(output.Sessions) != 2 || output.Sessions[1].ID != "second" {
		t.Fatalf("wrong history projection: %+v", result)
	}
	for _, limit := range []int{0, -1, 101} {
		t.Run(fmt.Sprintf("invalid limit %d", limit), func(t *testing.T) {
			invalid := callTool(
				t,
				client,
				"get_workout_history",
				map[string]any{"limit": limit},
			)
			if !invalid.IsError {
				t.Fatalf("invalid limit %d accepted", limit)
			}
		})
	}
	if repo.writeCount() != 0 {
		t.Fatal("history read mutated storage")
	}
}

func TestCatalogToolsAreScopedAndReadOnly(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	client := connect(t, service.New(repo), identity{owner: "catalog-reader", scopes: []string{mcptransport.ScopeScheduleRead}})
	search := callTool(t, client, "search_exercises", map[string]any{"limit": 1})
	if search.IsError {
		t.Fatalf("catalog search failed: %+v", search)
	}
	var page struct {
		Result service.ExerciseSearchResult `json:"result"`
	}
	page = decodeResult[struct {
		Result service.ExerciseSearchResult `json:"result"`
	}](t, search)
	if page.Result.CatalogVersion == "" || page.Result.Exercises == nil {
		t.Fatalf("catalog page = %+v", page)
	}
	if len(page.Result.Exercises) > 0 {
		first := page.Result.Exercises[0]
		detail := callTool(t, client, "get_exercise", map[string]any{"exercise_id": strings.ToUpper(first.Exercise.ID)})
		if detail.IsError {
			t.Fatalf("catalog detail failed: %+v", detail)
		}
		got := decodeResult[struct {
			Result service.ExerciseDetail `json:"result"`
		}](t, detail)
		if !reflect.DeepEqual(got.Result.Entry, first) {
			t.Fatal("search/detail snapshots differ")
		}
	}
	invalid := callTool(t, client, "search_exercises", map[string]any{"limit": 101})
	if !invalid.IsError {
		t.Fatal("unbounded search accepted")
	}
	denied := connect(t, service.New(repo), identity{owner: "catalog-reader", scopes: []string{mcptransport.ScopeWorkoutRead}})
	for _, name := range []string{"search_exercises", "get_exercise"} {
		input := map[string]any{}
		if name == "get_exercise" {
			input["exercise_id"] = exerciseID
		}
		result := callTool(t, denied, name, input)
		if !result.IsError || result.Meta["mcp/www_authenticate"] == nil {
			t.Fatalf("%s not scoped", name)
		}
	}
	if repo.writeCount() != 0 {
		t.Fatal("catalog tools wrote account data")
	}
}

func TestScheduleVolumeUsesCatalogWithoutPersistingDerivedData(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepository()
	svc := service.New(repo)
	plans := testPlans()
	plans[0].Exercise.ID = "00000000-0000-4000-8000-000000000001"
	plans[0].Exercise.Name = "Existing personal snapshot"
	_, err := svc.ReplaceWeek(t.Context(), "owner", weekStart, service.WeekWrite{
		ExpectedRevision: 0, RequestID: "50000000-0000-4000-8000-000000000018", Timezone: "Europe/Moscow",
		Workouts: []service.ScheduledWorkout{{ID: workoutID, PlannedDate: weekStart, Title: "Test", Exercises: plans}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := connect(t, svc, identity{owner: "owner", scopes: []string{mcptransport.ScopeScheduleRead}})
	result := callTool(t, client, "get_training_week", map[string]any{"week_start": weekStart, "timezone": "Europe/Moscow"})
	if result.IsError {
		t.Fatalf("week read failed: %+v", result)
	}
	got := decodeResult[struct {
		Week service.WeekView `json:"week"`
	}](t, result)
	if got.Week.PlannedMuscleVolume.TotalSets != 2 || got.Week.PlannedMuscleVolume.UnmappedExerciseCount != 0 || len(got.Week.PlannedMuscleVolume.Muscles) == 0 {
		t.Fatalf("wrong volume: %+v", got.Week.PlannedMuscleVolume)
	}
	if got.Week.Workouts[0].Exercises[0].Exercise.Name != "Existing personal snapshot" {
		t.Fatal("catalog changed saved snapshot")
	}
	stored, err := repo.Get(t.Context(), "owner", "week/"+weekStart)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored.Payload), "plannedMuscleVolume") {
		t.Fatal("derived volume persisted in user state")
	}
}
