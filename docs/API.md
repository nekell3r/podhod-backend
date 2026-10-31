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
| `PUT /v1/active` | `{expectedRevision,requestID,state,removedSetIDs?}` → active record |
| `POST /v1/active/rest` | `{expectedRevision,requestID,sessionID,exerciseID,restSeconds,restAfterExerciseSeconds}` → active record |
| `POST /v1/active/skip` | `{expectedRevision,requestID,sessionID,exerciseID,isSkipped}` → active record |
| `GET /v1/history` | `{sessions:[]}` |
| `PUT /v1/history/{sessionID}` | `{requestID,session,removedSetIDs?}` → immutable finished session |
| `GET /v1/protein/{date}` | `{date,revision,morningTakenAt?,eveningTakenAt?}` |
| `PUT /v1/protein/{date}` | `{expectedRevision,requestID,morningTakenAt?,eveningTakenAt?}` → protein record |
| `GET /v1/nutrition/catalog` | Public versioned food and recipe catalog, without authentication |
| `GET /v1/nutrition/days/{date}?timezone=Europe/Moscow` | `{revision,document,totals}`; timezone initializes only an absent day |
| `PUT /v1/nutrition/days/{date}` | `{expectedRevision,requestID,document}` → day with server-computed totals |
| `GET/PUT /v1/nutrition/goals` | Versioned goal collection, record/write envelope below |
| `GET/PUT /v1/nutrition/recipes` | Versioned own-recipe collection, record/write envelope below |
| `GET/PUT /v1/nutrition/preferences` | Versioned favorite-ID lists, record/write envelope below |

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


## Repetitions in reserve

Each `LoggedSet` optionally includes integer `repsInReserve`: `0` means failure, `1`–`4` are remaining repetitions, `5` means five or more, and `-1` explicitly removes an assessment. An absent field or JSON null means a legacy or never-assessed set; it must not be interpreted as zero. Values outside `-1...5` are rejected for completed sets and drafts. Effort does not change volume, repetition validation or timing.

For the same active session, omitting a previously present effort field returns HTTP 409 `upgrade_required`. Removing its entire set without an explicit removal intent has the same protection. A supporting client clears an assessment with `-1`. MCP rest/skip commands preserve the field. History append checks the matching active session under the same owner transaction lock and rejects legacy omission before inserting a finished snapshot. Existing completed history remains immutable, and identical retries remain idempotent. Explicitly clearing active state is still allowed for discard and after a successful history upload.

Android has no effort UI in this release. Legacy clients can read the optional field but cannot replace an assessed active session or finish it after discarding the field; use an updated iOS client for that session.

## Explicit unfinished-set removal

Active and history writes optionally carry `removedSetIDs`: at most 600 distinct UUIDs, case-insensitive, absent from the submitted session. A nonempty list requires a non-null active state. The list records the user's explicit removal of unfinished sets; it does not clear assessments on retained sets. The server compares the matching previous active session under its owner lock. A listed set that was unfinished may be absent even if it had an assessment; a listed set already completed on the server returns HTTP 409 `set_removal_conflict`. The write is rejected as a whole. CAS, owner isolation, request-ID replay and history immutability still apply.

IDs unknown to the current server snapshot are accepted because creation and deletion may both happen offline, or the deletion may already have reached active state before history upload. Clients must persist intents atomically with local removals and include them when finishing offline, even after an earlier active upload succeeded. Clear finished-session intents only after confirming the same history snapshot on the server. Do not infer consent from missing sets in older archives. This metadata belongs to write envelopes; active/history responses and stored session snapshots have no new fields. Deploy the supporting backend before releasing the new client. Clients that omit the list retain the existing protection.

## Nutrition documents

The public catalog embeds `internal/service/nutrition-catalog.json` unchanged in meaning: `formatVersion`, `version`, `foods`, `recipes`, `recipeAttribution`. It contains no owner data. All private nutrition GETs require `workout:read`, PUTs require `workout:write`; using these existing scopes was explicitly chosen by the user. Owner comes only from the verified bearer subject. Android and OAuth configuration are unchanged. Private JSON responses use `Cache-Control: no-store`.

Each private resource returns `{revision,document}`; day responses also add `totals`. PUT takes `{expectedRevision,requestID,document}`. Initial revision is 0; every document has `version:1`. Future formats return 409 `upgrade_required`. Invalid stored documents return 500 `invalid_resource` and cannot be overwritten. Writes use the existing owner transaction lock, CAS, request-ID replay and audit. Unknown JSON fields, extra JSON objects and bodies above 2 MiB are rejected. Per-item limits do not override the total body limit.

Day example (all four numeric nutrient fields are mandatory; absent/null is not zero):

```json
{
  "expectedRevision": 0,
  "requestID": "00000000-0000-4000-8000-000000000010",
  "document": {
    "version": 1,
    "date": "2026-10-31",
    "timezone": "Europe/Moscow",
    "goal": null,
    "entries": [{
      "id": "00000000-0000-4000-8000-000000000001",
      "meal": "breakfast",
      "grams": 10,
      "food": {
        "name": "Оливковое масло",
        "per100Grams": {"energyKcal": 884, "proteinGrams": 0, "fatGrams": 100, "carbohydrateGrams": 0},
        "origin": {"kind": "catalogFood", "id": "usda-171413", "catalogVersion": "2026.10.31.2"}
      }
    }]
  }
}
```

The returned `totals` has `energyKcal:88.4`, `proteinGrams:0`, `fatGrams:10`, `carbohydrateGrams:0`. It is computed from snapshots, never accepted from the client or recomputed from a newer catalog. Source energy is not replaced with 4/9/4. `origin.kind` is `catalogFood`, `catalogRecipe`, `ownRecipe` or `manual`. Catalog origins require ID and catalog version; foods use canonical positive `usda-{fdcID}`. Own recipes require a UUID and empty catalog version. Manual origins have empty ID/version. Historical catalog IDs/versions are not resolved against current food values, preserving old snapshots.

Days have up to 100 entries and valid dates from 1900 through 9999. The date must match the route. Meals are `breakfast/lunch/dinner/snack`; entry IDs are unique UUIDs. Mass is `(0,100000]` grams of edible food. Per 100 g, energy is 0–1000 kcal and each macro 0–100 g, with macro sum at most 101 g to allow source rounding. All numbers must be finite. `entries:[]` explicitly removes food; null/missing arrays are rejected. A cleared day retains its revision, protecting it from stale restoration. Initial GET timezone defaults to UTC if omitted; querying an existing day never changes its timezone.

A day's optional `goal` is a snapshot `{id,effectiveFrom,timezone,target}`. Target uses the four nutrient field names above, with energy `(0,20000]` kcal and macros 0–3000 g; these are technical input limits, not recommended intakes. The effective date must not be later than the day. After the first write, a day keeps its goal snapshot (including null) and timezone: changes return 409 `nutrition_goal_immutable` or `nutrition_day_immutable`. Entries remain editable with CAS. Editing/removing recipes or global goals never rewrites saved days.

Other document bodies:

- Goals: `{version:1,goals:[]}`, up to 100 `{id,effectiveFrom,timezone,target}` items with unique UUIDs and effective dates.
- Recipes: `{version:1,recipes:[]}`, up to 100 items. Each has UUID `id`, `name` (1–200 characters), `category` (`breakfast/main/salad/snack`), `minutes` (1–1440), positive `servings` up to 1000, positive `preparedWeightGrams` up to 100000, 1–50 `ingredients` and 1–30 `steps`. An ingredient is `{id,grams,food}` with its own UUID and the snapshot shape above. Steps contain 1–2000 characters. Server validates total/serving/ready-weight calculations; source nutrients remain unchanged when water changes ready weight.
- Preferences: `{version:1,favoriteRecipeIDs:[],favoriteFoodIDs:[]}`. At most 500 combined IDs, each nonempty and at most 200 characters, without surrounding whitespace. Lists are unique within each kind. Unknown historical/future IDs are retained.

To delete a goal, own recipe or favorite, PUT the explicit new collection with current revision and a fresh request UUID. A retry keeps its exact request ID/body/revision. Replaying an older successful write after deletion returns the original receipt without changing current data; clients must not downgrade local state from that older receipt. Recipe ingredients and diary foods store snapshots; no write infers consumed nutrition from protein-reminder marks. Nutrition uses the existing `training_resources` namespace, requiring no SQL migration.
