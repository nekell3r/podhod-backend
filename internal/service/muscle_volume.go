package service

type MuscleSetVolume struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	DirectSets   int     `json:"directSets"`
	IndirectSets int     `json:"indirectSets"`
	WeightedSets float64 `json:"weightedSets"`
}

type UnmappedPlannedExercise struct {
	ExerciseID string `json:"exerciseID"`
	Name       string `json:"name"`
	SetCount   int    `json:"setCount"`
}

type PlannedMuscleVolume struct {
	CatalogVersion        string                    `json:"catalogVersion"`
	TotalSets             int                       `json:"totalSets"`
	UnmappedExerciseCount int                       `json:"unmappedExerciseCount"`
	UnmappedSets          int                       `json:"unmappedSets"`
	Muscles               []MuscleSetVolume         `json:"muscles"`
	UnmappedExercises     []UnmappedPlannedExercise `json:"unmappedExercises"`
}

type TemplateView struct {
	Template
	PlannedMuscleVolume PlannedMuscleVolume `json:"plannedMuscleVolume"`
}

type WeekView struct {
	TrainingWeek
	PlannedMuscleVolume PlannedMuscleVolume `json:"plannedMuscleVolume"`
}

func (c ExerciseCatalog) PlannedVolume(plans []PlannedExercise) PlannedMuscleVolume {
	result := PlannedMuscleVolume{
		CatalogVersion: c.Version, Muscles: []MuscleSetVolume{},
		UnmappedExercises: []UnmappedPlannedExercise{},
	}
	volumes := make(map[string]MuscleSetVolume, len(c.Muscles))
	for _, plan := range plans {
		result.TotalSets += plan.SetCount
		entry, found := c.Entry(plan.Exercise.ID)
		if !found || len(entry.PrimaryMuscles)+len(entry.SecondaryMuscles) == 0 {
			result.UnmappedExerciseCount++
			result.UnmappedSets += plan.SetCount
			result.UnmappedExercises = append(result.UnmappedExercises, UnmappedPlannedExercise{
				ExerciseID: plan.Exercise.ID, Name: plan.Exercise.Name, SetCount: plan.SetCount,
			})
			continue
		}
		seen := make(map[string]bool, len(entry.PrimaryMuscles)+len(entry.SecondaryMuscles))
		for _, id := range entry.PrimaryMuscles {
			if seen[id] {
				continue
			}
			seen[id] = true
			volume := volumes[id]
			volume.ID = id
			volume.DirectSets += plan.SetCount
			volume.WeightedSets += float64(plan.SetCount)
			volumes[id] = volume
		}
		for _, id := range entry.SecondaryMuscles {
			if seen[id] {
				continue
			}
			seen[id] = true
			volume := volumes[id]
			volume.ID = id
			volume.IndirectSets += plan.SetCount
			volume.WeightedSets += float64(plan.SetCount) * 0.5
			volumes[id] = volume
		}
	}
	for _, muscle := range c.Muscles {
		if volume, exists := volumes[muscle.ID]; exists {
			volume.Name = muscle.Name
			result.Muscles = append(result.Muscles, volume)
		}
	}
	return result
}

func (s *Service) TemplateView(value Template) (TemplateView, error) {
	catalog, err := readExerciseCatalog()
	if err != nil {
		return TemplateView{}, err
	}
	plans := []PlannedExercise{}
	for _, day := range value.Days {
		plans = append(plans, day.Exercises...)
	}
	return TemplateView{Template: value, PlannedMuscleVolume: catalog.PlannedVolume(plans)}, nil
}

func (s *Service) WeekView(value TrainingWeek) (WeekView, error) {
	catalog, err := readExerciseCatalog()
	if err != nil {
		return WeekView{}, err
	}
	plans := []PlannedExercise{}
	for _, workout := range value.Workouts {
		plans = append(plans, workout.Exercises...)
	}
	return WeekView{TrainingWeek: value, PlannedMuscleVolume: catalog.PlannedVolume(plans)}, nil
}
