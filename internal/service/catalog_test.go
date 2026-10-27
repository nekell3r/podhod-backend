package service

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func catalogFixture() ExerciseCatalog {
	return ExerciseCatalog{Version: "fixture-v1", Muscles: []CatalogLabel{
		{ID: "chest", Name: "Грудь"}, {ID: "triceps", Name: "Трицепс"},
	}, Equipment: []CatalogLabel{{ID: "barbell", Name: "Штанга"}}, Exercises: []CatalogEntry{
		{Exercise: Exercise{ID: "aaaaaaaa-0000-4000-8000-000000000001", Name: "Жим лёжа"},
			Aliases: []string{"Bench press"}, PrimaryMuscles: []string{"chest", "chest"},
			SecondaryMuscles: []string{"chest", "triceps", "triceps"}, Equipment: []string{"barbell"}},
		{Exercise: Exercise{ID: "aaaaaaaa-0000-4000-8000-000000000002", Name: "Разгибание"},
			PrimaryMuscles: []string{"triceps"}},
	}}
}

func TestCatalogSearchFiltersAndPagination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		input       ExerciseSearch
		total, page int
		invalid     bool
	}{
		{name: "all", input: ExerciseSearch{}, total: 2, page: 2},
		{name: "russian case and yo", input: ExerciseSearch{Query: "  ЛЕЖА "}, total: 1, page: 1},
		{name: "english alias", input: ExerciseSearch{Query: "BENCH"}, total: 1, page: 1},
		{name: "muscle name", input: ExerciseSearch{Query: "грудь"}, total: 1, page: 1},
		{name: "equipment name", input: ExerciseSearch{Query: "штанга"}, total: 1, page: 1},
		{name: "secondary filter", input: ExerciseSearch{Muscle: "triceps"}, total: 2, page: 2},
		{name: "and filters", input: ExerciseSearch{Muscle: "triceps", Equipment: "barbell"}, total: 1, page: 1},
		{name: "unknown filter", input: ExerciseSearch{Muscle: "missing"}},
		{name: "page", input: ExerciseSearch{Offset: 1, Limit: 1}, total: 2, page: 1},
		{name: "past end", input: ExerciseSearch{Offset: 9}, total: 2},
		{name: "negative offset", input: ExerciseSearch{Offset: -1}, invalid: true},
		{name: "excessive page", input: ExerciseSearch{Limit: 101}, invalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := catalogFixture().Search(tc.input)
			if (err != nil) != tc.invalid {
				t.Fatalf("error = %v, invalid = %v", err, tc.invalid)
			}
			if err == nil && (got.Total != tc.total || len(got.Exercises) != tc.page || got.Exercises == nil) {
				t.Fatalf("result = %+v, want total %d and page %d", got, tc.total, tc.page)
			}
		})
	}
}

func TestPlannedVolumeDeduplicatesAndKeepsUnknownSets(t *testing.T) {
	t.Parallel()
	catalog := catalogFixture()
	plans := []PlannedExercise{
		{Exercise: Exercise{ID: "AAAAAAAA-0000-4000-8000-000000000001", Name: "old snapshot"}, SetCount: 4},
		{Exercise: catalog.Exercises[1].Exercise, SetCount: 3},
		{Exercise: Exercise{ID: "custom", Name: "custom"}, SetCount: 2},
		{Exercise: Exercise{ID: "custom", Name: "custom"}, SetCount: 1},
	}
	before := append([]PlannedExercise{}, plans...)
	got := catalog.PlannedVolume(plans)
	wantMuscles := []MuscleSetVolume{
		{ID: "chest", Name: "Грудь", DirectSets: 4, WeightedSets: 4},
		{ID: "triceps", Name: "Трицепс", DirectSets: 3, IndirectSets: 4, WeightedSets: 5},
	}
	if got.TotalSets != 10 || got.UnmappedSets != 3 || got.UnmappedExerciseCount != 2 {
		t.Fatalf("wrong coverage: %+v", got)
	}
	if !reflect.DeepEqual(got.Muscles, wantMuscles) || !reflect.DeepEqual(plans, before) {
		t.Fatalf("volume = %+v, expected %+v; snapshot mutated = %v", got.Muscles, wantMuscles, !reflect.DeepEqual(plans, before))
	}
}

func TestPlannedVolumeEmptyAndKnownUnmapped(t *testing.T) {
	t.Parallel()
	catalog := catalogFixture()
	empty := catalog.PlannedVolume(nil)
	if empty.Muscles == nil || empty.UnmappedExercises == nil || empty.TotalSets != 0 {
		t.Fatalf("empty volume = %+v", empty)
	}
	catalog.Exercises[0].PrimaryMuscles = nil
	catalog.Exercises[0].SecondaryMuscles = nil
	got := catalog.PlannedVolume([]PlannedExercise{{Exercise: catalog.Exercises[0].Exercise, SetCount: 4}})
	if got.UnmappedExerciseCount != 1 || got.UnmappedSets != 4 || len(got.Muscles) != 0 {
		t.Fatalf("unmapped known = %+v", got)
	}
}

func TestBundledCatalogAssetsAndStableLegacyIDs(t *testing.T) {
	t.Parallel()
	catalog, err := readExerciseCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Exercises) != 58 || len(catalog.Muscles) != 18 || len(catalog.Equipment) != 11 {
		t.Fatalf("unexpected catalog dimensions: exercises=%d muscles=%d equipment=%d", len(catalog.Exercises), len(catalog.Muscles), len(catalog.Equipment))
	}
	muscles := map[string]bool{}
	for _, muscle := range catalog.Muscles {
		if muscle.ID == "" || muscle.Name == "" || muscles[muscle.ID] {
			t.Fatalf("invalid muscle %+v", muscle)
		}
		muscles[muscle.ID] = true
	}
	equipment := map[string]bool{}
	for _, item := range catalog.Equipment {
		if item.ID == "" || item.Name == "" || equipment[item.ID] {
			t.Fatalf("invalid equipment %+v", item)
		}
		equipment[item.ID] = true
	}
	ids := map[string]bool{}
	videos, covered, native, external := 0, 0, 0, 0
	for i, entry := range catalog.Exercises {
		id := strings.ToLower(entry.Exercise.ID)
		if !validUUID(id) || ids[id] || entry.Exercise.Name == "" {
			t.Fatalf("invalid exercise %q", id)
		}
		ids[id] = true
		if i < 12 && id != fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1) {
			t.Fatalf("legacy ID %d changed: %s", i, id)
		}
		if len(entry.Instructions) == 0 || len(entry.PrimaryMuscles) == 0 {
			t.Fatalf("missing instruction/mapping: %s", id)
		}
		for _, muscle := range append(append([]string{}, entry.PrimaryMuscles...), entry.SecondaryMuscles...) {
			if !muscles[muscle] {
				t.Fatalf("unknown muscle %s in %s", muscle, id)
			}
		}
		for _, item := range entry.Equipment {
			if !equipment[item] {
				t.Fatalf("unknown equipment %s", item)
			}
		}
		if len(entry.Videos) > 0 {
			covered++
		}
		for _, video := range entry.Videos {
			videos++
			if video.Playback != nil && *video.Playback == "external" {
				external++
			} else {
				native++
				if !strings.HasPrefix(video.URL, "https://wger.de/") {
					t.Fatal("native video source changed")
				}
			}
			url, err := url.Parse(video.URL)
			if err != nil || url.Scheme != "https" || video.Author == "" || video.LicenseURL == "" || video.SourceURL == "" {
				t.Fatalf("invalid video attribution: %+v", video)
			}
		}
	}
	if videos != 89 || native != 78 || external != 11 || covered < 46 {
		t.Fatalf("videos=%d covered=%d", videos, covered)
	}
}

func TestCatalogExerciseIDAliasesResolveWithoutDuplicatingSearchOrChangingPlans(t *testing.T) {
	t.Parallel()
	catalog := catalogFixture()
	alias := "bbbbbbbb-0000-4000-8000-000000000001"
	catalog.ExerciseIDAliases = []ExerciseIDAlias{{ID: alias, ExerciseID: catalog.Exercises[0].Exercise.ID}}
	if err := catalog.validate(); err != nil {
		t.Fatal(err)
	}
	entry, found := catalog.Entry(strings.ToUpper(alias))
	if !found || !reflect.DeepEqual(entry, catalog.Exercises[0]) {
		t.Fatal("alias did not resolve to canonical entry")
	}
	search, err := catalog.Search(ExerciseSearch{})
	if err != nil || search.Total != 2 {
		t.Fatalf("aliases duplicated search: %+v %v", search, err)
	}
	plans := []PlannedExercise{{Exercise: Exercise{ID: alias, Name: "Saved custom snapshot"}, SetCount: 3}}
	volume := catalog.PlannedVolume(plans)
	if volume.TotalSets != 3 || volume.UnmappedSets != 0 || volume.Muscles[0].DirectSets != 3 {
		t.Fatalf("alias volume = %+v", volume)
	}
	if plans[0].Exercise.ID != alias || plans[0].Exercise.Name != "Saved custom snapshot" {
		t.Fatal("alias changed persisted snapshot")
	}
}

func TestCatalogRejectsUnsafeAliasTables(t *testing.T) {
	t.Parallel()
	first := "aaaaaaaa-0000-4000-8000-000000000001"
	alias := "bbbbbbbb-0000-4000-8000-000000000001"
	secondAlias := "bbbbbbbb-0000-4000-8000-000000000002"
	for _, tc := range []struct {
		name    string
		aliases []ExerciseIDAlias
	}{
		{name: "invalid alias UUID", aliases: []ExerciseIDAlias{{ID: "invalid", ExerciseID: first}}},
		{name: "canonical collision", aliases: []ExerciseIDAlias{{ID: first, ExerciseID: first}}},
		{name: "missing canonical target", aliases: []ExerciseIDAlias{{ID: alias, ExerciseID: secondAlias}}},
		{name: "duplicate case insensitive", aliases: []ExerciseIDAlias{{ID: alias, ExerciseID: first}, {ID: strings.ToUpper(alias), ExerciseID: first}}},
		{name: "chain", aliases: []ExerciseIDAlias{{ID: alias, ExerciseID: first}, {ID: secondAlias, ExerciseID: alias}}},
		{name: "cycle", aliases: []ExerciseIDAlias{{ID: alias, ExerciseID: secondAlias}, {ID: secondAlias, ExerciseID: alias}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := catalogFixture()
			catalog.ExerciseIDAliases = tc.aliases
			if err := catalog.validate(); err == nil {
				t.Fatal("unsafe aliases accepted")
			}
		})
	}
}

func TestCatalogVideoPlaybackAndHTTPSValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, playback, url, source string
		valid                       bool
	}{
		{name: "legacy native", url: "https://wger.de/video.mov", valid: true},
		{name: "explicit native is unsupported", playback: "native", url: "https://wger.de/video.mov"},
		{name: "external watch page", playback: "external", url: "https://www.youtube.com/watch?v=example", valid: true},
		{name: "unknown mode", playback: "iframe", url: "https://example.com/video"},
		{name: "explicit empty is unsupported", url: "https://example.com/video"},
		{name: "insecure media", url: "http://example.com/video"},
		{name: "javascript", playback: "external", url: "javascript:alert(1)"},
		{name: "credential URL", playback: "external", url: "https://user:password@example.com/watch"},
		{name: "missing URL", playback: "external"},
		{name: "insecure source", url: "https://example.com/video", source: "http://example.com/source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := catalogFixture()
			var playback *string
			if tc.playback != "" || tc.name == "explicit empty is unsupported" {
				playback = &tc.playback
			}
			catalog.Exercises[0].Videos = []ExerciseVideo{{ID: "video", URL: tc.url, Playback: playback, ExerciseAttribution: ExerciseAttribution{SourceURL: tc.source}}}
			if err := catalog.validate(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
