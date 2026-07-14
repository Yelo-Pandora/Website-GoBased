// Package course provides course catalog and theory content services.
package course

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound indicates that a course does not exist.
var ErrNotFound = errors.New("course not found")

// Course is a course catalog item.
type Course struct {
	ID           uint64 `json:"id"`
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Category     string `json:"category"`
	Status       string `json:"status"`
	SortOrder    int    `json:"sortOrder"`
	Summary      string `json:"summary"`
	LabAvailable bool   `json:"labAvailable"`
	Progress     any    `json:"progress"`
}

// Detail contains the catalog item and its static theory content.
type Detail struct {
	Course
	Content        string         `json:"content"`
	ContentFormat  string         `json:"contentFormat"`
	Implementation Implementation `json:"implementation"`
	Lab            Lab            `json:"lab"`
}

// Implementation describes the observable backend path for a course.
type Implementation struct {
	RequestPath []string `json:"requestPath"`
	KeyConcepts []string `json:"keyConcepts"`
}

// Lab describes whether and how a course can start a lab.
type Lab struct {
	Available            bool           `json:"available"`
	ScenarioType         string         `json:"scenarioType,omitempty"`
	AllowedInstanceRange *InstanceRange `json:"allowedInstanceRange,omitempty"`
}

// InstanceRange is the allowed application instance range for a lab.
type InstanceRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Repository reads course metadata from the platform database.
type Repository struct {
	database *sql.DB
}

// NewRepository returns a MySQL-backed course repository.
func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// List returns all courses in knowledge-map order.
func (r *Repository) List(ctx context.Context) ([]Course, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT id, slug, title, category, status, sort_order, summary
		FROM courses
		ORDER BY sort_order, id`)
	if err != nil {
		return nil, fmt.Errorf("list courses: %w", err)
	}
	defer rows.Close()

	courses := make([]Course, 0)
	for rows.Next() {
		course, err := scanCourse(rows)
		if err != nil {
			return nil, err
		}
		courses = append(courses, course)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate courses: %w", err)
	}
	return courses, nil
}

// GetBySlug returns one course by its stable slug.
func (r *Repository) GetBySlug(ctx context.Context, slug string) (Course, error) {
	row := r.database.QueryRowContext(ctx, `
		SELECT id, slug, title, category, status, sort_order, summary
		FROM courses
		WHERE slug = ?`, slug)
	course, err := scanCourse(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Course{}, ErrNotFound
	}
	if err != nil {
		return Course{}, err
	}
	return course, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCourse(row rowScanner) (Course, error) {
	var course Course
	if err := row.Scan(
		&course.ID,
		&course.Slug,
		&course.Title,
		&course.Category,
		&course.Status,
		&course.SortOrder,
		&course.Summary,
	); err != nil {
		return Course{}, fmt.Errorf("scan course: %w", err)
	}
	course.LabAvailable = course.Status == "active"
	return course, nil
}

// Service combines course metadata with embedded theory content.
type Service struct {
	repository *Repository
	content    *ContentStore
}

// NewService returns a course application service.
func NewService(repository *Repository, content *ContentStore) *Service {
	return &Service{repository: repository, content: content}
}

// List returns the knowledge-map catalog.
func (s *Service) List(ctx context.Context) ([]Course, error) {
	return s.repository.List(ctx)
}

// Get returns course metadata, theory content, and lab metadata.
func (s *Service) Get(ctx context.Context, slug string) (Detail, error) {
	slug = strings.TrimSpace(slug)
	if !validSlug(slug) {
		return Detail{}, ErrNotFound
	}

	course, err := s.repository.GetBySlug(ctx, slug)
	if err != nil {
		return Detail{}, err
	}
	content, implementation, lab := s.content.For(course)
	return Detail{
		Course:         course,
		Content:        content,
		ContentFormat:  "markdown",
		Implementation: implementation,
		Lab:            lab,
	}, nil
}

func validSlug(slug string) bool {
	if slug == "" || len(slug) > 128 {
		return false
	}
	for _, r := range slug {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}
