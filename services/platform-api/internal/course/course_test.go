package course

import (
	"context"
	"testing"
	"time"
)

type repositoryStub struct {
	course          Course
	markedUserID    uint64
	markedCourseID  uint64
	markedViewedAt  time.Time
	markViewedCalls int
}

func (r *repositoryStub) List(
	_ context.Context,
	_ *uint64,
) ([]Course, error) {
	return []Course{r.course}, nil
}

func (r *repositoryStub) GetBySlug(
	_ context.Context,
	_ string,
	_ *uint64,
) (Course, error) {
	return r.course, nil
}

func (r *repositoryStub) MarkViewed(
	_ context.Context,
	userID uint64,
	courseID uint64,
	viewedAt time.Time,
) error {
	r.markViewedCalls++
	r.markedUserID = userID
	r.markedCourseID = courseID
	r.markedViewedAt = viewedAt
	return nil
}

func TestGetMarksAuthenticatedCourseViewed(t *testing.T) {
	now := time.Date(2026, time.July, 15, 9, 0, 0, 123456789, time.UTC)
	wantViewedAt := now.Truncate(time.Microsecond)
	repository := &repositoryStub{course: Course{
		ID:     1,
		Slug:   "standalone-architecture",
		Status: "theory",
	}}
	service := newService(repository, NewContentStore())
	service.now = func() time.Time { return now }
	userID := uint64(7)

	detail, err := service.Get(
		context.Background(),
		"standalone-architecture",
		&userID,
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if repository.markViewedCalls != 1 ||
		repository.markedUserID != userID ||
		repository.markedCourseID != 1 {
		t.Fatalf("MarkViewed() record = %#v", repository)
	}
	if !repository.markedViewedAt.Equal(wantViewedAt) {
		t.Fatalf("viewedAt = %v; want %v", repository.markedViewedAt, wantViewedAt)
	}
	if detail.Progress == nil || !detail.Progress.Viewed ||
		detail.Progress.LastViewedAt == nil ||
		!detail.Progress.LastViewedAt.Equal(wantViewedAt) {
		t.Fatalf("progress = %#v", detail.Progress)
	}
}

func TestGetAnonymousDoesNotWriteProgress(t *testing.T) {
	repository := &repositoryStub{course: Course{
		ID:     1,
		Slug:   "standalone-architecture",
		Status: "theory",
	}}
	service := newService(repository, NewContentStore())

	detail, err := service.Get(
		context.Background(),
		"standalone-architecture",
		nil,
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if repository.markViewedCalls != 0 {
		t.Fatalf("MarkViewed() calls = %d; want 0", repository.markViewedCalls)
	}
	if detail.Progress != nil {
		t.Fatalf("progress = %#v; want nil", detail.Progress)
	}
}
