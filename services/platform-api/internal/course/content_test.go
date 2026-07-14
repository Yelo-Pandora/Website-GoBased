package course

import (
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
		"cache-failures",
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
