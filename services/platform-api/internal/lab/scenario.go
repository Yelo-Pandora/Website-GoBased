package lab

type scenario struct {
	Type                string
	TemplateID          string
	InitialInstances    int
	TemporaryContainers int
	RedisRequired       bool
}

var scenariosByCourseSlug = map[string]scenario{
	"application-data-separation": {
		Type:                "application_data_separation",
		TemplateID:          "application_data_separation_scenario_v1",
		InitialInstances:    1,
		TemporaryContainers: 1,
	},
	"application-cluster": {
		Type:                "application_cluster",
		TemplateID:          "application_cluster_scenario_v1",
		InitialInstances:    1,
		TemporaryContainers: 1,
	},
	"multi-level-cache": {
		Type:                "multi_level_cache",
		TemplateID:          "multi_level_cache_scenario_v1",
		InitialInstances:    1,
		TemporaryContainers: 2,
		RedisRequired:       true,
	},
	"cache-failures": {
		Type:                "cache_failures",
		TemplateID:          "cache_failures_scenario_v1",
		InitialInstances:    1,
		TemporaryContainers: 2,
		RedisRequired:       true,
	},
}

func scenarioForCourse(slug, status string) (scenario, bool) {
	if status != "active" {
		return scenario{}, false
	}
	value, ok := scenariosByCourseSlug[slug]
	return value, ok
}
