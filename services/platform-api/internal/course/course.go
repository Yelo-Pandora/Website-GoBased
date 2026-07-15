// Package course provides course catalog and theory content services.
package course

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound indicates that a course does not exist.
var ErrNotFound = errors.New("course not found")

// Course is a course catalog item.
type Course struct {
	ID           uint64    `json:"id"`
	Slug         string    `json:"slug"`
	Title        string    `json:"title"`
	Category     string    `json:"category"`
	Status       string    `json:"status"`
	SortOrder    int       `json:"sortOrder"`
	Summary      string    `json:"summary"`
	LabAvailable bool      `json:"labAvailable"`
	Progress     *Progress `json:"progress"`
}

// Progress is one user's persisted learning progress for a course.
type Progress struct {
	Viewed       bool       `json:"viewed"`
	LastViewedAt *time.Time `json:"lastViewedAt"`
	LastLabID    *string    `json:"lastLabId"`
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

// List returns all courses and optional user progress in knowledge-map order.
func (r *Repository) List(
	ctx context.Context,
	userID *uint64,
) ([]Course, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT
			c.id,
			c.slug,
			c.title,
			c.category,
			c.status,
			c.sort_order,
			c.summary,
			p.id,
			p.viewed,
			p.last_viewed_at,
			p.last_lab_id
		FROM courses AS c
		LEFT JOIN course_progress AS p
		  ON p.course_id = c.id
		 AND p.user_id = ?
		ORDER BY c.sort_order, c.id`, optionalUserID(userID))
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

// GetBySlug returns one course and optional user progress by stable slug.
func (r *Repository) GetBySlug(
	ctx context.Context,
	slug string,
	userID *uint64,
) (Course, error) {
	row := r.database.QueryRowContext(ctx, `
		SELECT
			c.id,
			c.slug,
			c.title,
			c.category,
			c.status,
			c.sort_order,
			c.summary,
			p.id,
			p.viewed,
			p.last_viewed_at,
			p.last_lab_id
		FROM courses AS c
		LEFT JOIN course_progress AS p
		  ON p.course_id = c.id
		 AND p.user_id = ?
		WHERE c.slug = ?`, optionalUserID(userID), slug)
	course, err := scanCourse(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Course{}, ErrNotFound
	}
	if err != nil {
		return Course{}, err
	}
	return course, nil
}

// MarkViewed records that one user viewed a course at a specific time.
func (r *Repository) MarkViewed(
	ctx context.Context,
	userID uint64,
	courseID uint64,
	viewedAt time.Time,
) error {
	if _, err := r.database.ExecContext(ctx, `
		INSERT INTO course_progress (
			user_id,
			course_id,
			viewed,
			last_viewed_at
		) VALUES (?, ?, TRUE, ?)
		ON DUPLICATE KEY UPDATE
			viewed = TRUE,
			last_viewed_at = ?`,
		userID,
		courseID,
		viewedAt,
		viewedAt,
	); err != nil {
		return fmt.Errorf("mark course viewed: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCourse(row rowScanner) (Course, error) {
	var course Course
	var progressID sql.NullInt64
	var viewed sql.NullBool
	var lastViewedAt sql.NullTime
	var lastLabID sql.NullString
	if err := row.Scan(
		&course.ID,
		&course.Slug,
		&course.Title,
		&course.Category,
		&course.Status,
		&course.SortOrder,
		&course.Summary,
		&progressID,
		&viewed,
		&lastViewedAt,
		&lastLabID,
	); err != nil {
		return Course{}, fmt.Errorf("scan course: %w", err)
	}
	course.LabAvailable = course.Status == "active"
	if progressID.Valid {
		course.Progress = &Progress{Viewed: viewed.Bool}
		if lastViewedAt.Valid {
			value := lastViewedAt.Time.UTC()
			course.Progress.LastViewedAt = &value
		}
		if lastLabID.Valid {
			value := lastLabID.String
			course.Progress.LastLabID = &value
		}
	}
	return course, nil
}

func optionalUserID(userID *uint64) uint64 {
	if userID == nil {
		return 0
	}
	return *userID
}

type repository interface {
	List(ctx context.Context, userID *uint64) ([]Course, error)
	GetBySlug(ctx context.Context, slug string, userID *uint64) (Course, error)
	MarkViewed(
		ctx context.Context,
		userID uint64,
		courseID uint64,
		viewedAt time.Time,
	) error
}

// Service combines course metadata with embedded theory content.
type Service struct {
	repository repository
	content    *ContentStore
	now        func() time.Time
}

// NewService returns a course application service.
func NewService(repository *Repository, content *ContentStore) *Service {
	return newService(repository, content)
}

func newService(repository repository, content *ContentStore) *Service {
	return &Service{
		repository: repository,
		content:    content,
		now:        time.Now,
	}
}

// List returns the knowledge-map catalog.
func (s *Service) List(ctx context.Context, userID *uint64) ([]Course, error) {
	return s.repository.List(ctx, userID)
}

// Get returns course metadata, theory content, and lab metadata.
func (s *Service) Get(
	ctx context.Context,
	slug string,
	userID *uint64,
) (Detail, error) {
	slug = strings.TrimSpace(slug)
	if !validSlug(slug) {
		return Detail{}, ErrNotFound
	}

	course, err := s.repository.GetBySlug(ctx, slug, userID)
	if err != nil {
		return Detail{}, err
	}
	if userID != nil {
		viewedAt := s.now().UTC().Truncate(time.Microsecond)
		if err := s.repository.MarkViewed(
			ctx,
			*userID,
			course.ID,
			viewedAt,
		); err != nil {
			return Detail{}, err
		}
		if course.Progress == nil {
			course.Progress = &Progress{}
		}
		course.Progress.Viewed = true
		course.Progress.LastViewedAt = &viewedAt
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
