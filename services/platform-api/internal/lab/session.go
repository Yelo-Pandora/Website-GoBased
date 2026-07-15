package lab

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrAlreadyActive indicates that the user already owns an active lab.
	ErrAlreadyActive = errors.New("user already has an active lab")
	// ErrCourseNotFound indicates that the requested course does not exist.
	ErrCourseNotFound = errors.New("lab course not found")
	// ErrLabUnavailable indicates that the course has no active lab scenario.
	ErrLabUnavailable = errors.New("lab is unavailable for the course")
	// ErrNotFound indicates that a lab session does not exist.
	ErrNotFound = errors.New("lab session not found")
	// ErrNotOwned indicates that a lab session belongs to another user.
	ErrNotOwned = errors.New("lab session is not owned by the user")
	// ErrOperationConflict indicates that an operation ID was reused incompatibly.
	ErrOperationConflict = errors.New("operation id conflicts with an existing operation")
	// ErrStateConflict indicates that persisted state changed before an update.
	ErrStateConflict = errors.New("lab state changed concurrently")
)

// Session is the persistent control-plane view of one lab.
type Session struct {
	ID                    string     `json:"id"`
	UserID                uint64     `json:"-"`
	CourseID              uint64     `json:"courseId"`
	ScenarioType          string     `json:"scenarioType"`
	ScenarioTemplateID    string     `json:"scenarioTemplateId"`
	Status                Status     `json:"status"`
	BalancingMode         string     `json:"balancingMode"`
	RedisEnabled          bool       `json:"redisEnabled"`
	StartedAt             *time.Time `json:"startedAt"`
	LastEffectiveActionAt *time.Time `json:"lastEffectiveActionAt"`
	TerminatedAt          *time.Time `json:"terminatedAt"`
	TerminationReason     *string    `json:"terminationReason"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

// CreatedOperation is the operation created atomically with a lab session.
type CreatedOperation struct {
	ID          uint64     `json:"-"`
	OperationID string     `json:"operationId"`
	LabID       string     `json:"labId"`
	RequestedBy uint64     `json:"-"`
	Action      string     `json:"action"`
	Status      string     `json:"status"`
	SubmittedAt time.Time  `json:"submittedAt"`
	CompletedAt *time.Time `json:"completedAt"`
}

// CreateResult contains one idempotent lab creation result.
type CreateResult struct {
	Session   Session
	Operation CreatedOperation
	Existing  bool
}

type createRepository interface {
	Create(
		ctx context.Context,
		userID uint64,
		courseID uint64,
		operationID string,
		labID string,
		quota Quota,
		now time.Time,
	) (CreateResult, error)
	FindOwned(ctx context.Context, labID string, userID uint64) (Session, error)
}

// Service applies lab session rules before using persistent storage.
type Service struct {
	repository createRepository
	quota      Quota
	now        func() time.Time
	newID      func() (string, error)
}

// NewService returns a MySQL-backed lab session service.
func NewService(repository *Repository, quota Quota) *Service {
	return newService(repository, quota)
}

func newService(repository createRepository, quota Quota) *Service {
	return &Service{
		repository: repository,
		quota:      quota,
		now:        time.Now,
		newID:      newLabID,
	}
}

// Create creates a Preparing session and its CREATE_LAB operation atomically.
func (s *Service) Create(
	ctx context.Context,
	userID uint64,
	courseID uint64,
	operationID string,
) (CreateResult, error) {
	if userID == 0 || courseID == 0 || !validOperationID(operationID) {
		return CreateResult{}, errors.New("invalid lab creation request")
	}
	if err := s.quota.Validate(); err != nil {
		return CreateResult{}, err
	}
	labID, err := s.newID()
	if err != nil {
		return CreateResult{}, err
	}
	return s.repository.Create(
		ctx,
		userID,
		courseID,
		operationID,
		labID,
		s.quota,
		s.now().UTC().Truncate(time.Microsecond),
	)
}

// GetOwned returns a lab only when it belongs to the authenticated user.
func (s *Service) GetOwned(
	ctx context.Context,
	labID string,
	userID uint64,
) (Session, error) {
	if !validLabID(labID) || userID == 0 {
		return Session{}, ErrNotFound
	}
	return s.repository.FindOwned(ctx, labID, userID)
}
