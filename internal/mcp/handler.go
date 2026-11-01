package mcptransport

import (
	"context"
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"podhod/server/internal/service"
)

type handler struct {
	service             *service.Service
	resourceMetadataURL string
}

func NewHandler(svc *service.Service, resourceMetadataURL string) *sdkmcp.StreamableHTTPHandler {
	h := &handler{service: svc, resourceMetadataURL: resourceMetadataURL}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "podhod", Version: "1.2.1"}, nil)

	falseHint := false
	trueHint := true
	readHints := &sdkmcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: &falseHint,
		OpenWorldHint:   &falseHint,
	}
	editHints := &sdkmcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: &falseHint,
		IdempotentHint:  true,
		OpenWorldHint:   &falseHint,
	}
	replaceHints := &sdkmcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: &trueHint,
		IdempotentHint:  true,
		OpenWorldHint:   &falseHint,
	}

	profileMeta := oauthMetadata("")
	profileMeta["openai/profile"] = true
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "get_profile",
		Title: "Connected Podhod account",
		Description: "Return the profile authenticated by this connection. The opaque id is stable across " +
			"token refresh and reconnection. No user ID argument is accepted.",
		Annotations: readHints,
		Meta:        profileMeta,
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type": "string", "minLength": 1, "pattern": `\S`,
				},
			},
			"required":             []string{"id"},
			"additionalProperties": false,
		},
	}, h.profile)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "get_training_template",
		Title: "Read recurring training program",
		Description: "Read the recurring Monday-to-Sunday training template for the connected account. This " +
			"is distinct from a dated week and does not change any data. plannedMuscleVolume is a planned set estimate: directSets plus 0.5 * indirectSets, not measured physiological load. Unknown custom exercises are explicitly unmapped.",
		Annotations: readHints,
		Meta:        oauthMetadata(ScopeScheduleRead),
	}, h.getTemplate)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "replace_training_template",
		Title: "Replace recurring training program",
		Description: "Replace the recurring weekly template after reading its current revision. Does not " +
			"change already saved dated weeks, active sessions, or workout history. Use " +
			"replace_training_week to change only one particular week.",
		Annotations: replaceHints,
		Meta:        oauthMetadata(ScopeScheduleWrite),
	}, h.replaceTemplate)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "get_training_week",
		Title: "Read a dated training week",
		Description: "Read the connected account's schedule for the week beginning on the specified Monday. " +
			"Supply YYYY-MM-DD and an IANA timezone. This read never creates a week or changes the " +
			"recurring template. Includes plannedMuscleVolume using direct + 0.5 * indirect set counts; custom exercises with unknown IDs remain unmapped.",
		Annotations: readHints,
		Meta:        oauthMetadata(ScopeScheduleRead),
	}, h.getWeek)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "replace_training_week",
		Title: "Replace one dated training week",
		Description: "Replace the planned workouts for exactly one specified week. Read it first and send " +
			"its expected_revision. Keep existing IDs when retaining workouts and exercises. This " +
			"does not edit the recurring template, an active workout snapshot, or history.",
		Annotations: replaceHints,
		Meta:        oauthMetadata(ScopeScheduleWrite),
	}, h.replaceWeek)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "move_scheduled_workout",
		Title: "Move a planned workout",
		Description: "Move one scheduled workout to another date in the same specified week, preserving its " +
			"exercises and ID. An occupied day is a conflict, never an implicit overwrite or swap. " +
			"Read the week's latest revision first. Does not move a started or finished session.",
		Annotations: editHints,
		Meta:        oauthMetadata(ScopeScheduleWrite),
	}, h.moveWorkout)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "update_planned_exercise_rest",
		Title: "Set rest in a planned workout",
		Description: "Set rest between sets and rest after an exercise in one dated planned workout. " +
			"Durations are seconds. Read the week's latest revision first. Does not change an " +
			"active session's snapshot or an already running timer.",
		Annotations: editHints,
		Meta:        oauthMetadata(ScopeScheduleWrite),
	}, h.setScheduledRest)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "get_active_workout",
		Title: "Read active workout",
		Description: "Read the latest server snapshot of the connected account's active workout and its " +
			"revision. A null state means there is no active workout on the server. An offline " +
			"iPhone may have local changes that have not synchronized yet. Optional timing contains " +
			"phone-observed exercise-screen, rest-timer and other intervals; physical set start/end " +
			"times are not measured. Missing timing is unknown. Only the iPhone advances these " +
			"intervals; MCP commands remain pending until the phone synchronizes.",
		Annotations: readHints,
		Meta:        oauthMetadata(ScopeWorkoutRead),
	}, h.getActive)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "update_active_exercise_rest",
		Title: "Set rest in the active workout",
		Description: "Set rest between sets and after an exercise in the identified active session. " +
			"Requires the current session_id and expected_revision. A currently running timer " +
			"keeps its deadline; changes affect future rests. The iPhone receives server changes " +
			"on its next successful sync.",
		Annotations: editHints,
		Meta:        oauthMetadata(ScopeWorkoutWrite),
	}, h.setActiveRest)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "skip_active_exercise",
		Title: "Skip an active exercise",
		Description: "Skip the remaining part of one exercise in the identified active session. Preserves " +
			"all entered and completed sets. Skipped sets are not counted as completed. Requires " +
			"current session_id and expected_revision. Updates server state; the iPhone receives " +
			"the change on sync, not immediately while offline.",
		Annotations: editHints,
		Meta:        oauthMetadata(ScopeWorkoutWrite),
	}, h.skipExercise)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "unskip_active_exercise",
		Title: "Undo an exercise skip",
		Description: "Remove the skipped flag from one exercise in the identified active session while " +
			"retaining its recorded sets. Requires current session_id and expected_revision. Does " +
			"not mark sets completed. The iPhone receives server changes on its next successful " +
			"sync.",
		Annotations: editHints,
		Meta:        oauthMetadata(ScopeWorkoutWrite),
	}, h.unskipExercise)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:  "get_workout_history",
		Title: "Read completed workout history",
		Description: "Read recent completed workout snapshots for the connected account. This is read-only " +
			"and cannot edit historical results. Supply a limit from 1 to 100; the default is 20. " +
			"Optional timing records phone-observed UI/rest intervals, not physical set durations. " +
			"Missing timing is unknown and must not be reconstructed from configured rest settings. " +
			"MCP does not measure or advance the phone's recorded clock.",
		Annotations: readHints,
		Meta:        oauthMetadata(ScopeWorkoutRead),
	}, h.history)

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "search_exercises", Title: "Search exercise catalog",
		Description: "Search the Russian exercise catalog by name or alias, muscle and equipment. " +
			"Returns ready Exercise snapshots to put in planned exercises, instructions, licensed video URLs " +
			"and catalog muscle mappings. Videos with playback=external are author watch pages to open externally; a missing playback field means native licensed media. Limit 1–100, default 20. Does not modify any plan or history.",
		Annotations: readHints, Meta: oauthMetadata(ScopeScheduleRead),
	}, h.searchExercises)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "get_exercise", Title: "Read exercise catalog entry",
		Description: "Read an exercise by its catalog UUID, with a ready Exercise snapshot, instructions, " +
			"muscle mappings and video references. playback=external identifies an author watch page, not a native media stream or a redistribution license. Aliased saved UUIDs resolve to canonical entries without changing the saved exercise or its ID. Does not rewrite historical or active exercise snapshots.",
		Annotations: readHints, Meta: oauthMetadata(ScopeScheduleRead),
	}, h.getExercise)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "get_nutrition_catalog", Title: "Read food and recipe catalog",
		Description: "Read the curated foods and recipes with exact IDs, preparation state, per-100-g KBJU, " +
			"ingredients and source/license metadata. Raw and cooked foods are distinct. Never fill unknown nutrition with zero.",
		Annotations: readHints, Meta: oauthMetadata(ScopeWorkoutRead),
	}, h.getNutritionCatalog)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "get_nutrition_recipes", Title: "Read own recipes",
		Description: "Read the connected account's own recipes and collection revision before creating, editing or deleting a recipe. " +
			"Does not include the shared catalog; use get_nutrition_catalog for that. Phone-only unsynchronized edits are not visible.",
		Annotations: readHints, Meta: oauthMetadata(ScopeWorkoutRead),
	}, h.getNutritionRecipes)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "create_nutrition_recipe", Title: "Create an own recipe",
		Description: "Add one own recipe, preserving other recipes. Read get_nutrition_recipes first for expected_revision. " +
			"Supply explicit ingredient KBJU per 100 g, ingredient grams, servings and measured preparedWeightGrams; " +
			"do not invent missing amounts or nutrition. New recipe and ingredient IDs must be UUIDs. " +
			"A repeated identical request_id is safe; a duplicate recipe ID is an error. The iPhone receives it on sync.",
		Annotations: editHints, Meta: oauthMetadata(ScopeWorkoutWrite),
	}, h.createNutritionRecipe)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "update_nutrition_recipe", Title: "Edit an own recipe",
		Description: "Edit exactly one existing own recipe after reading get_nutrition_recipes. Send its recipe_id, " +
			"the complete edited recipe, expected_revision of the collection and UUID request_id. Preserve recipe UUID " +
			"and retained ingredient UUIDs; include unchanged fields. Names, category, time, servings, ingredients, " +
			"preparedWeightGrams and steps may change. Explicit KBJU and actual ready weight are required; do not invent missing values. " +
			"Other recipes, favorites and existing diary snapshots are preserved. A missing recipe is an error, never an implicit create. " +
			"Retry an uncertain response with identical arguments and request_id; reconcile revision conflicts before a new action. " +
			"The iPhone receives the change on its next successful sync.",
		Annotations: replaceHints, Meta: oauthMetadata(ScopeWorkoutWrite),
	}, h.updateNutritionRecipe)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "delete_nutrition_recipe", Title: "Delete an own recipe",
		Description: "Delete exactly the own recipe UUID requested by the user after reading get_nutrition_recipes. " +
			"Preserves other recipes and all existing diary snapshots, goals and favorites. Cannot delete shared catalog recipes. " +
			"Requires expected_revision and UUID request_id. The iPhone receives the change on its next successful sync.",
		Annotations: replaceHints, Meta: oauthMetadata(ScopeWorkoutWrite),
	}, h.deleteNutritionRecipe)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "get_nutrition_day", Title: "Read food diary day",
		Description: "Read one YYYY-MM-DD food diary day, saved goal/timezone, totals and revision for the connected account. " +
			"Does not create a day. Resolve relative dates using the user's IANA timezone. Read before logging food.",
		Annotations: readHints, Meta: oauthMetadata(ScopeWorkoutRead),
	}, h.getNutritionDay)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "get_nutrition_goals", Title: "Read nutrition goal history",
		Description: "Read saved manual nutrition goals. Before the first entry of an unrecorded day, select the goal " +
			"with the latest effectiveFrom not after the day, or null when none, for log_nutrition_entry.initial_goal. " +
			"An already recorded day keeps its own saved goal, including no goal.",
		Annotations: readHints, Meta: oauthMetadata(ScopeWorkoutRead),
	}, h.getNutritionGoals)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "log_nutrition_entry", Title: "Log food eaten",
		Description: "Append exactly one eaten food or dish snapshot without replacing other entries. Read get_nutrition_day " +
			"for expected_revision first. Resolve date, meal and grams with the user; do not guess missing KBJU as zero. " +
			"Use catalog, own recipe calculations, or explicit label values per 100 g. For revision 0, read get_nutrition_goals " +
			"and provide initial_goal; existing goal/timezone snapshots are preserved. Requires a new entry UUID and request_id UUID. " +
			"Reuse the identical request_id on retry, never log again with a new ID after an uncertain response. " +
			"A conflict requires reading the day again and reconciling; the iPhone receives the result on sync.",
		Annotations: editHints, Meta: oauthMetadata(ScopeWorkoutWrite),
	}, h.logNutritionEntry)
	return sdkmcp.NewStreamableHTTPHandler(func(_ *http.Request) *sdkmcp.Server {
		return server
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

type emptyInput struct{}

type profileResult struct {
	ID string `json:"id"`
}

func (h *handler) profile(
	_ context.Context,
	req *sdkmcp.CallToolRequest,
	_ emptyInput,
) (*sdkmcp.CallToolResult, any, error) {
	owner, denied := authorizeTool(req, "", h.resourceMetadataURL)
	if denied != nil {
		return denied, nil, nil
	}
	return nil, profileResult{ID: owner}, nil
}
