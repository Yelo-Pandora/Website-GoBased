package lab

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidRequest indicates that a lab request contains invalid identity fields.
	ErrInvalidRequest = errors.New("invalid lab request")
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
	// ErrBusy indicates that another serialized lab operation is in progress.
	ErrBusy = errors.New("lab has a pending operation")
	// ErrNotRunning indicates that the requested action requires a running lab.
	ErrNotRunning = errors.New("lab is not running")
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

// Instance is the persistent topology state for one application server.
type Instance struct {
	ID                 string  `json:"instanceId"`
	Name               string  `json:"instanceName"`
	ContainerID        string  `json:"-"`
	Status             string  `json:"status"`
	CPULimitCores      float64 `json:"cpuLimitCores"`
	MemoryLimitMB      int     `json:"memoryLimitMb"`
	PerformancePercent int     `json:"performancePercent"`
	EffectiveCapacity  int     `json:"effectiveCapacity"`
	CurrentWeight      int     `json:"currentWeight"`
}

// Resource is a persisted non-application resource owned by a lab.
type Resource struct {
	Type       string
	Name       string
	ExternalID string
	Status     string
	Metadata   map[string]any
}

// ResourceStatus is the public state of an optional topology resource.
type ResourceStatus struct {
	Status string `json:"status"`
}

// OperationError is the stable error portion of a lab operation snapshot.
type OperationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// OperationSnapshot is the latest asynchronous operation for a lab.
type OperationSnapshot struct {
	OperationID      string          `json:"operationId"`
	Action           string          `json:"action"`
	TargetInstanceID *string         `json:"targetInstanceId,omitempty"`
	Status           string          `json:"status"`
	SubmittedAt      time.Time       `json:"submittedAt"`
	CompletedAt      *time.Time      `json:"completedAt,omitempty"`
	Error            *OperationError `json:"error,omitempty"`
}

// Topology is the user-visible resource arrangement of a lab.
type Topology struct {
	Instances []Instance      `json:"instances"`
	Redis     *ResourceStatus `json:"redis"`
	Gateway   struct {
		Status string `json:"status"`
	} `json:"gateway"`
}

// Snapshot contains the state required to restore a lab page after a refresh.
type Snapshot struct {
	Lab             Session            `json:"lab"`
	Topology        Topology           `json:"topology"`
	Resources       []Resource         `json:"-"`
	LatestOperation *OperationSnapshot `json:"latestOperation"`
}

// CreatedOperation is the operation created atomically with a lab session.
type CreatedOperation struct {
	ID               uint64     `json:"-"`
	OperationID      string     `json:"operationId"`
	LabID            string     `json:"labId"`
	RequestedBy      uint64     `json:"-"`
	Action           string     `json:"action"`
	TargetInstanceID *string    `json:"targetInstanceId,omitempty"`
	Status           string     `json:"status"`
	SubmittedAt      time.Time  `json:"submittedAt"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

// CreateResult contains one idempotent lab creation result.
type CreateResult struct {
	Session   Session
	Operation CreatedOperation
	Existing  bool
}

// ActionResult contains one idempotent asynchronous lab action.
type ActionResult struct {
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

type actionRepository interface {
	EnqueueAction(
		ctx context.Context,
		userID uint64,
		labID string,
		operationID string,
		action string,
		payload any,
		now time.Time,
	) (ActionResult, error)
	FindSnapshot(ctx context.Context, labID string, userID uint64) (Snapshot, error)
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
		return CreateResult{}, ErrInvalidRequest
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

// Reset enqueues an idempotent reset operation for a running lab.
func (s *Service) Reset(
	ctx context.Context,
	userID uint64,
	labID string,
	operationID string,
) (ActionResult, error) {
	if userID == 0 || !validLabID(labID) || !validOperationID(operationID) {
		return ActionResult{}, ErrInvalidRequest
	}
	repository, ok := s.repository.(actionRepository)
	if !ok {
		return ActionResult{}, errors.New("lab repository does not support actions")
	}
	return repository.EnqueueAction(
		ctx, userID, labID, operationID, "RESET_LAB",
		map[string]any{"scenarioTemplateId": ""},
		s.now().UTC().Truncate(time.Microsecond),
	)
}

// Terminate enqueues an idempotent destroy operation for a lab.
func (s *Service) Terminate(
	ctx context.Context,
	userID uint64,
	labID string,
	operationID string,
) (ActionResult, error) {
	if userID == 0 || !validLabID(labID) || !validOperationID(operationID) {
		return ActionResult{}, ErrInvalidRequest
	}
	repository, ok := s.repository.(actionRepository)
	if !ok {
		return ActionResult{}, errors.New("lab repository does not support actions")
	}
	return repository.EnqueueAction(
		ctx, userID, labID, operationID, "DESTROY_LAB", map[string]any{},
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

// Snapshot returns a complete owned lab snapshot for page restoration.
func (s *Service) Snapshot(
	ctx context.Context,
	labID string,
	userID uint64,
) (Snapshot, error) {
	if !validLabID(labID) || userID == 0 {
		return Snapshot{}, ErrNotFound
	}
	repository, ok := s.repository.(actionRepository)
	if !ok {
		return Snapshot{}, errors.New("lab repository does not support snapshots")
	}
	return repository.FindSnapshot(ctx, labID, userID)
}
