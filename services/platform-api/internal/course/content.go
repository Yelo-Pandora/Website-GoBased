package course

import (
	"embed"
	"fmt"
)

//go:embed content/*.md
var contentFiles embed.FS

type contentDefinition struct {
	file           string
	implementation Implementation
	lab            Lab
}

// ContentStore reads immutable course content embedded in the API binary.
type ContentStore struct {
	definitions map[string]contentDefinition
}

// NewContentStore returns the built-in MVP course content store.
func NewContentStore() *ContentStore {
	return &ContentStore{definitions: map[string]contentDefinition{
		"standalone-architecture": {
			file: "content/standalone-architecture.md",
			implementation: Implementation{
				RequestPath: []string{"edge", "application", "database"},
				KeyConcepts: []string{"single-host", "vertical-scaling", "single-point-of-failure"},
			},
			lab: Lab{Available: false},
		},
		"application-data-separation": {
			file: "content/application-data-separation.md",
			implementation: Implementation{
				RequestPath: []string{"lab-gateway-nginx", "lab-app", "shared-mysql"},
				KeyConcepts: []string{"separation-of-responsibilities", "network-boundary", "independent-scaling"},
			},
			lab: labDefinition("application_data_separation", 1, 1),
		},
		"application-cluster": {
			file: "content/application-cluster.md",
			implementation: Implementation{
				RequestPath: []string{"lab-gateway-nginx", "lab-app", "shared-mysql"},
				KeyConcepts: []string{"weighted-balancing", "health-check", "effective-capacity"},
			},
			lab: labDefinition("application_cluster", 1, 4),
		},
		"multi-level-cache": {
			file: "content/multi-level-cache.md",
			implementation: Implementation{
				RequestPath: []string{"lab-app-l1", "session-redis", "shared-mysql"},
				KeyConcepts: []string{"cache-aside", "cache-invalidation", "bounded-ttl"},
			},
			lab: labDefinition("multi_level_cache", 1, 3),
		},
		"cache-failures": {
			file: "content/cache-failures.md",
			implementation: Implementation{
				RequestPath: []string{"lab-app-l1", "session-redis", "shared-mysql"},
				KeyConcepts: []string{"cache-penetration", "cache-breakdown", "cache-avalanche", "redis-fallback"},
			},
			lab: labDefinition("cache_failures", 1, 3),
		},
	}}
}

// For returns theory and experiment metadata for a course.
func (s *ContentStore) For(course Course) (string, Implementation, Lab) {
	definition, ok := s.definitions[course.Slug]
	if !ok {
		return comingSoonContent(course), Implementation{}, Lab{Available: false}
	}
	content, err := contentFiles.ReadFile(definition.file)
	if err != nil {
		return comingSoonContent(course), Implementation{}, Lab{Available: false}
	}
	return string(content), definition.implementation, definition.lab
}

func labDefinition(scenarioType string, minInstances, maxInstances int) Lab {
	return Lab{
		Available:    true,
		ScenarioType: scenarioType,
		AllowedInstanceRange: &InstanceRange{
			Min: minInstances,
			Max: maxInstances,
		},
	}
}

func comingSoonContent(course Course) string {
	return fmt.Sprintf(
		"# %s\n\n%s\n\n这门课程仍在准备中。当前页面不会创建实验资源。\n",
		course.Title,
		course.Summary,
	)
}
