package service

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

//go:embed exercise-catalog.json
var catalogJSON []byte

type CatalogLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ExerciseAttribution struct {
	Author     string `json:"author"`
	License    string `json:"license"`
	LicenseURL string `json:"licenseURL"`
	SourceURL  string `json:"sourceURL"`
}

type ExerciseVideo struct {
	ID       string  `json:"id"`
	URL      string  `json:"url"`
	Playback *string `json:"playback,omitempty"`
	ExerciseAttribution
}

type CatalogEntry struct {
	Exercise         Exercise            `json:"exercise"`
	Aliases          []string            `json:"aliases"`
	PrimaryMuscles   []string            `json:"primaryMuscles"`
	SecondaryMuscles []string            `json:"secondaryMuscles"`
	Equipment        []string            `json:"equipment"`
	Instructions     []string            `json:"instructions"`
	Videos           []ExerciseVideo     `json:"videos"`
	SourceURL        string              `json:"sourceURL"`
	Attribution      ExerciseAttribution `json:"attribution"`
}

type ExerciseIDAlias struct {
	ID         string `json:"id"`
	ExerciseID string `json:"exerciseID"`
}

type ExerciseCatalog struct {
	ExerciseIDAliases []ExerciseIDAlias `json:"exerciseIDAliases,omitempty"`
	Version           string            `json:"version"`
	Muscles           []CatalogLabel    `json:"muscles"`
	Equipment         []CatalogLabel    `json:"equipment"`
	Exercises         []CatalogEntry    `json:"exercises"`
}

type ExerciseSearch struct {
	Query     string
	Muscle    string
	Equipment string
	Limit     int
	Offset    int
}

type ExerciseSearchResult struct {
	CatalogVersion string         `json:"catalogVersion"`
	Total          int            `json:"total"`
	Limit          int            `json:"limit"`
	Offset         int            `json:"offset"`
	Muscles        []CatalogLabel `json:"muscles"`
	Equipment      []CatalogLabel `json:"equipment"`
	Exercises      []CatalogEntry `json:"exercises"`
}

type ExerciseDetail struct {
	CatalogVersion string       `json:"catalogVersion"`
	Entry          CatalogEntry `json:"entry"`
}

func readExerciseCatalog() (ExerciseCatalog, error) {
	var catalog ExerciseCatalog
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		return catalog, fmt.Errorf("decode bundled exercise catalog: %w", err)
	}
	if err := catalog.validate(); err != nil {
		return catalog, fmt.Errorf("validate bundled exercise catalog: %w", err)
	}
	return catalog, nil
}

func (s *Service) SearchExercises(input ExerciseSearch) (ExerciseSearchResult, error) {
	catalog, err := readExerciseCatalog()
	if err != nil {
		return ExerciseSearchResult{}, err
	}
	return catalog.Search(input)
}

func (c ExerciseCatalog) Search(input ExerciseSearch) (ExerciseSearchResult, error) {
	if input.Limit == 0 {
		input.Limit = 20
	}
	if input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return ExerciseSearchResult{}, invalid("limit must be 1–100 and offset nonnegative")
	}
	query := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(input.Query)), "ё", "е")
	if len(query) > 300 {
		return ExerciseSearchResult{}, invalid("query must be at most 300 bytes")
	}
	result := ExerciseSearchResult{
		CatalogVersion: c.Version, Limit: input.Limit, Offset: input.Offset,
		Muscles: c.Muscles, Equipment: c.Equipment, Exercises: []CatalogEntry{},
	}
	for _, entry := range c.Exercises {
		matchesMuscle := input.Muscle == "" || slices.Contains(entry.PrimaryMuscles, input.Muscle) ||
			slices.Contains(entry.SecondaryMuscles, input.Muscle)
		matchesEquipment := input.Equipment == "" || slices.Contains(entry.Equipment, input.Equipment)
		if !matchesMuscle || !matchesEquipment {
			continue
		}
		terms := append([]string{entry.Exercise.Name, entry.Exercise.Muscle}, entry.Aliases...)
		for _, muscle := range c.Muscles {
			if slices.Contains(entry.PrimaryMuscles, muscle.ID) || slices.Contains(entry.SecondaryMuscles, muscle.ID) {
				terms = append(terms, muscle.Name)
			}
		}
		for _, equipment := range c.Equipment {
			if slices.Contains(entry.Equipment, equipment.ID) {
				terms = append(terms, equipment.Name)
			}
		}
		searchText := strings.ReplaceAll(strings.ToLower(strings.Join(terms, " ")), "ё", "е")
		if query != "" && !strings.Contains(searchText, query) {
			continue
		}
		if result.Total >= input.Offset && len(result.Exercises) < input.Limit {
			result.Exercises = append(result.Exercises, entry)
		}
		result.Total++
	}
	return result, nil
}

func (s *Service) GetExercise(id string) (ExerciseDetail, error) {
	catalog, err := readExerciseCatalog()
	if err != nil {
		return ExerciseDetail{}, err
	}
	if entry, found := catalog.Entry(id); found {
		return ExerciseDetail{CatalogVersion: catalog.Version, Entry: entry}, nil
	}
	return ExerciseDetail{}, &Error{Code: "not_found", Message: "exercise not found in catalog"}
}

func (c ExerciseCatalog) Entry(id string) (CatalogEntry, bool) {
	for _, alias := range c.ExerciseIDAliases {
		if strings.EqualFold(alias.ID, id) {
			id = alias.ExerciseID
			break
		}
	}
	for _, entry := range c.Exercises {
		if strings.EqualFold(entry.Exercise.ID, id) {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

func (c ExerciseCatalog) validate() error {
	canonical := make(map[string]bool, len(c.Exercises))
	for _, entry := range c.Exercises {
		id := strings.ToLower(entry.Exercise.ID)
		if !validUUID(id) || canonical[id] {
			return fmt.Errorf("invalid or duplicate catalog exercise UUID")
		}
		canonical[id] = true
		urls := []string{entry.SourceURL, entry.Attribution.SourceURL, entry.Attribution.LicenseURL}
		for _, video := range entry.Videos {
			if video.Playback != nil && *video.Playback != "external" {
				return fmt.Errorf("unsupported video playback mode")
			}
			if video.URL == "" {
				return fmt.Errorf("video URL is required")
			}
			urls = append(urls, video.URL, video.SourceURL, video.LicenseURL)
		}
		for _, raw := range urls {
			if raw == "" {
				continue
			}
			parsed, err := url.Parse(raw)
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
				return fmt.Errorf("catalog web URLs must use HTTPS without credentials")
			}
		}
	}
	aliases := make(map[string]bool, len(c.ExerciseIDAliases))
	for _, alias := range c.ExerciseIDAliases {
		id, target := strings.ToLower(alias.ID), strings.ToLower(alias.ExerciseID)
		if !validUUID(id) || !validUUID(target) || canonical[id] || aliases[id] || !canonical[target] {
			return fmt.Errorf("alias UUID must be unique, noncanonical and target a canonical exercise directly")
		}
		aliases[id] = true
	}
	return nil
}
