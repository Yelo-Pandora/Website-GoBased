package course

import (
	"slices"
	"strings"
	"testing"
)

func TestContentStoreContainsMVPTheory(t *testing.T) {
	store := NewContentStore()
	slugs := []string{
		"standalone-architecture",
		"application-data-separation",
		"application-cluster",
		"multi-level-cache",
	}

	for _, slug := range slugs {
		t.Run(slug, func(t *testing.T) {
			content, _, _ := store.For(Course{Slug: slug, Title: slug})
			if len(strings.TrimSpace(content)) < 200 {
				t.Fatalf("content length = %d; want complete theory", len(content))
			}
			if strings.Contains(content, "Caffeine") {
				t.Fatal("content includes excluded cache implementation")
			}
		})
	}
}

func TestContentStoreReturnsComingSoonContent(t *testing.T) {
	store := NewContentStore()
	content, _, lab := store.For(Course{
		Slug:    "future-course",
		Title:   "后续课程",
		Summary: "课程占位。",
	})

	if !strings.Contains(content, "仍在准备中") {
		t.Fatalf("content = %q; want coming-soon message", content)
	}
	if lab.Available {
		t.Fatal("coming-soon lab is available")
	}
}

func TestMultiLevelCacheContainsFailureTheory(t *testing.T) {
	store := NewContentStore()
	content, implementation, lab := store.For(Course{Slug: "multi-level-cache", Title: "多级缓存"})
	for _, topic := range []string{"缓存穿透", "缓存击穿", "缓存雪崩", "Redis 重启"} {
		if !strings.Contains(content, topic) {
			t.Fatalf("multi-level cache theory does not contain %q", topic)
		}
	}
	if !lab.Available || lab.ScenarioType != "multi_level_cache" {
		t.Fatalf("multi-level cache lab = %#v; want active cache lab", lab)
	}
	if !slices.Contains(implementation.KeyConcepts, "cache-avalanche") {
		t.Fatalf("key concepts = %#v; want merged cache failure concepts", implementation.KeyConcepts)
	}
}
