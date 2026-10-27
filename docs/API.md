# REST API contract

## API

JSON fields use camelCase. All date-only keys are `YYYY-MM-DD`; week starts must be Mondays. Timestamp fields use RFC3339/ISO8601. UUID comparisons for workout and exercise identities are case-insensitive.

| Method/path | Body / response |
|---|---|
| `GET /v1/config?platform=ios\|android` | Public `{issuer,clientID,scopes}`; omitted platform preserves iOS |
| `GET /v1/profile` | `{id,timezone}` from verified identity |
| `GET /v1/template` | `{revision,days,plannedMuscleVolume}` with seven weekday entries |
| `PUT /v1/template` | `{expectedRevision,requestID,days}` → template |
| `GET /v1/weeks/{weekStart}?timezone=Europe/Moscow` | `{weekStart,timezone,revision,workouts,plannedMuscleVolume}` |
| `PUT /v1/weeks/{weekStart}` | `{expectedRevision,requestID,timezone,workouts}` → week |
| `POST /v1/weeks/{weekStart}/move` | `{expectedRevision,requestID,workoutID,plannedDate}` → week |
| `POST /v1/weeks/{weekStart}/rest` | `{expectedRevision,requestID,workoutID,exerciseID,restSeconds,restAfterExerciseSeconds}` → week |
| `GET /v1/active` | `{revision,state}`; absent state is `null` |
| `PUT /v1/active` | `{expectedRevision,requestID,state}` → active record |
| `POST /v1/active/rest` | `{expectedRevision,requestID,sessionID,exerciseID,restSeconds,restAfterExerciseSeconds}` → active record |
| `POST /v1/active/skip` | `{expectedRevision,requestID,sessionID,exerciseID,isSkipped}` → active record |
| `GET /v1/history` | `{sessions:[]}` |
| `PUT /v1/history/{sessionID}` | `{requestID,session}` → immutable finished session |
| `GET /v1/protein/{date}` | `{date,revision,morningTakenAt?,eveningTakenAt?}` |
| `PUT /v1/protein/{date}` | `{expectedRevision,requestID,morningTakenAt?,eveningTakenAt?}` → protein record |

`GET /v1/exercises?query=&muscle=&equipment=&limit=20&offset=0` returns
`{catalogVersion,total,limit,offset,muscles,equipment,exercises}`. Limit is 1–100;
offset is nonnegative. Search matches Russian names (including ё/е), English aliases,
and resolved muscle/equipment labels. Filters combine with AND; a muscle may be primary
or secondary. `GET /v1/exercises/{uuid}` returns `{catalogVersion,entry}` or 404.
Both routes require `schedule:read`. MCP exposes the same data through read-only
`search_exercises` and `get_exercise`; their results wrap the catalog response in `result`.

The separate bundled catalog is embedded from `internal/service/exercise-catalog.json`;
it is the authoritative source for client exports. Clients pin its Git revision and SHA-256 in `catalog.lock.json`. It contains 58 Russian
entries, preserving the original 12 UUIDs, with 78 wger videos and 11 external author
references. Native videos stream from their licensed source URLs; external references
open the original author page. Each carries its own rights and source attribution.
Optional root `exerciseIDAliases` maps a saved UUID to one canonical exercise UUID. Alias
IDs are unique, cannot collide with canonical IDs and must target a canonical entry directly;
chains and cycles are rejected. Detail lookup and planned volume resolve aliases case-insensitively;
search still lists only canonical entries. This does not migrate or rewrite any saved UUID, cue or
personal exercise snapshot. Alias tables contain identities only, never private program text.

Video `playback:"external"` means an HTTPS author watch page, opened externally rather than passed
to a native media player. Missing playback preserves legacy native-video behavior; other values
are rejected. External references do not assert a Creative Commons redistribution license and
must not be downloaded or republished. All nonempty catalog web URLs use HTTPS without credentials.

Catalog metadata does not extend the persisted five-field `Exercise` snapshot. Active
sessions and history are never hydrated or rewritten from newer catalog entries.

All template/week read and write responses add `plannedMuscleVolume` without storing it
in PostgreSQL. The summary has `catalogVersion,totalSets,muscles,unmappedExerciseCount,
unmappedSets,unmappedExercises`. Each muscle has `id,name,directSets,indirectSets,weightedSets`:
weighted sets are direct sets plus 0.5 times indirect sets. Muscle membership is deduplicated
per exercise, with primary winning over secondary. Unknown/custom IDs remain explicitly
unmapped; names are never guessed. These are planned set estimates, not measured physiological
load or completed workout totals.

Schedule routes require `schedule:read` or `schedule:write`; active/history/protein routes require the corresponding `workout:*` scope. Empty template reads contain seven empty days; empty week/protein reads return revision zero without writing records.

Every mutable resource has a separate revision. Writes use an expected revision and UUID request ID. PostgreSQL serializes owner mutations transactionally and records idempotent results atomically with the update. A stale revision returns HTTP 409 with `{error:{code:"revision_conflict",message,currentRevision}}`; it never silently overwrites a newer state. Reusing a request ID for a different operation is rejected. One scheduled workout per day is supported; moves remain within the selected week and an occupied target returns 409.

Calendar edits do not rewrite active or completed workout snapshots. Skipping an active exercise preserves completed sets and incomplete drafts; skipping an already completed exercise is rejected. Unskipping selects that exercise again. Rest-setting changes preserve an already running timer. Appending history does not clear active state: clients upload history first, then explicitly clear active state with its own CAS. Finished sessions accept identical replays and reject changed content.

## Workout timing

`WorkoutSession.timing` is optional. Missing timing in legacy active workouts and history remains unknown; the server does not reconstruct exercise or rest durations from configured rest settings. New timing has `{version:1,trackedFrom,intervals:[...]}`. Each interval contains a UUID `id`, `kind` (`exercise`, `rest`, `other`), `startedAt`, optional `endedAt`, and a session-owned `exerciseID` for exercise/rest. Rest intervals additionally require `restKind` (`betweenSets` or `betweenExercises`) and their captured `restDeadline`.

The active phone (iOS or Android) is the measurement authority. MCP skip/unskip/rest commands preserve timing fields unchanged, even if the phone is offline or an open rest deadline has expired. A cloud command remains `pending_sync`; the phone settles its own old context before applying that command. The server must not fabricate exercise transitions using its receipt time.

The validator accepts gaps, explicit empty interval arrays and zero-length closed intervals. Intervals must be ordered, unique, nonoverlapping, no earlier than `trackedFrom`, and within the session's bounds. Only the final interval of an active session may remain open. Finished history requires all intervals closed. Recorded rest cannot extend past its captured deadline; the deadline itself may extend past a manually finished session. There are no persisted duration counters to double-count. Legacy clients cannot remove already recorded timing from the same active session: such a write returns HTTP 409 `upgrade_required`. Clearing active state and immutable-history behavior remain unchanged.

