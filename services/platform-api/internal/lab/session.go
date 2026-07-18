package lab

import (
	"context"
	"errors"
	"time"

	"website-gobased/internal/protocol"
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
	// ErrActionNotAllowed indicates that an action is unavailable for the scenario or mode.
	ErrActionNotAllowed = errors.New("lab action is not allowed")
	// ErrInstanceNotFound indicates that a target instance does not exist.
	ErrInstanceNotFound = errors.New("lab instance was not found")
	// ErrMinInstanceLimit indicates that removing an instance would leave no application server.
	ErrMinInstanceLimit = errors.New("lab minimum instance limit reached")
	// ErrMaxInstanceLimit indicates that adding an instance would exceed the lab limit.
	ErrMaxInstanceLimit = errors.New("lab maximum instance limit reached")
	// ErrWeightInvalid indicates that fixed weights are incomplete or outside the allowed range.
	ErrWeightInvalid = errors.New("lab instance weights are invalid")
)

const (
	ActionAddInstance            = "ADD_INSTANCE"
	ActionRemoveInstance         = "REMOVE_INSTANCE"
	ActionSetInstancePerformance = "SET_INSTANCE_PERFORMANCE"
	ActionSetInstanceWeights     = "SET_INSTANCE_WEIGHTS"
	ActionSetBalancingMode       = "SET_BALANCING_MODE"
)

// InstanceWeight is one fixed Nginx weight selected by the learner.
type InstanceWeight struct {
	InstanceID string `json:"instanceId"`
	Weight     int    `json:"weight"`
}

// ActionInput is one validated user-facing topology action request.
type ActionInput struct {
	OperationID        string           `json:"operationId"`
	ActionType         string           `json:"actionType"`
	TargetInstanceID   *string          `json:"targetInstanceId,omitempty"`
	PerformancePercent *int             `json:"performancePercent,omitempty"`
	Weights            []InstanceWeight `json:"weights,omitempty"`
	BalancingMode      *string          `json:"balancingMode,omitempty"`
}

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
	IdleExpiresAt         *time.Time `json:"idleExpiresAt"`
	MaximumExpiresAt      *time.Time `json:"maximumExpiresAt"`
	TerminatedAt          *time.Time `json:"terminatedAt"`
	TerminationReason     *string    `json:"terminationReason"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

// Instance is the persistent topology state for one application server.
type Instance struct {
	ID                 string    `json:"instanceId"`
	Name               string    `json:"instanceName"`
	ContainerID        string    `json:"-"`
	Status             string    `json:"status"`
	CPULimitCores      float64   `json:"cpuLimitCores"`
	MemoryLimitMB      int       `json:"memoryLimitMb"`
	PerformancePercent int       `json:"performancePercent"`
	ProcessingSpeed    int       `json:"processingSpeed"`
	MaxLoad            int       `json:"maxLoad"`
	CurrentWeight      int       `json:"currentWeight"`
	CurrentLoad        float64   `json:"currentLoad"`
	LoadRatio          float64   `json:"loadRatio"`
	LoadState          string    `json:"loadState"`
	ObservedAt         time.Time `json:"observedAt"`
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

// InstanceLoad is one smoothed load value exposed by the adaptive controller.
type InstanceLoad struct {
	InstanceID string  `json:"instanceId"`
	LoadRatio  float64 `json:"loadRatio"`
}

// BalancerSnapshot is the simplified public adaptive controller state.
type BalancerSnapshot struct {
	Status         string           `json:"status"`
	TargetWeights  []InstanceWeight `json:"targetWeights"`
	SmoothedLoads  []InstanceLoad   `json:"smoothedLoads"`
	CapacityNotice string           `json:"capacityNotice,omitempty"`
	LastError      *OperationError  `json:"lastError,omitempty"`
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
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
}

// TopologyNode is one semantic node used by the traffic visualization.
type TopologyNode struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// TopologyEdge is one directed semantic traffic connection.
type TopologyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// IntegerPolicy is one bounded learner-controlled numeric setting.
type IntegerPolicy struct {
	Minimum int `json:"minimum"`
	Maximum int `json:"maximum"`
	Step    int `json:"step"`
	Default int `json:"default"`
}

// TrafficPolicy exposes safe request-size and generation controls.
type TrafficPolicy struct {
	RequestUnits         IntegerPolicy `json:"requestUnits"`
	GenerationIntervalMS IntegerPolicy `json:"generationIntervalMs"`
}

// Snapshot contains the state required to restore a lab page after a refresh.
type Snapshot struct {
	Lab             Session              `json:"lab"`
	Topology        Topology             `json:"topology"`
	Resources       []Resource           `json:"-"`
	LatestOperation *OperationSnapshot   `json:"latestOperation"`
	TrafficPolicy   TrafficPolicy        `json:"trafficPolicy"`
	Balancer        *BalancerSnapshot    `json:"balancer"`
	Cache           *protocol.CacheState `json:"cache,omitempty"`
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

type topologyActionRepository interface {
	EnqueueTopologyAction(
		ctx context.Context,
		userID uint64,
		labID string,
		input ActionInput,
		quota Quota,
		now time.Time,
	) (ActionResult, error)
}

type snapshotDecorator interface {
	DecorateSnapshot(*Snapshot)
}

// Action enqueues one idempotent stage-seven topology action.
func (s *Service) Action(
	ctx context.Context,
	userID uint64,
	labID string,
	input ActionInput,
) (ActionResult, error) {
	if userID == 0 || !validLabID(labID) || !validOperationID(input.OperationID) ||
		!validActionInput(input) {
		return ActionResult{}, ErrInvalidRequest
	}
	repository, ok := s.repository.(topologyActionRepository)
	if !ok {
		return ActionResult{}, errors.New("lab repository does not support topology actions")
	}
	return repository.EnqueueTopologyAction(
		ctx,
		userID,
		labID,
		input,
		s.quota,
		s.now().UTC().Truncate(time.Microsecond),
	)
}

func validActionInput(input ActionInput) bool {
	switch input.ActionType {
	case ActionAddInstance:
		return input.TargetInstanceID == nil && input.PerformancePercent == nil &&
			len(input.Weights) == 0 && input.BalancingMode == nil
	case ActionRemoveInstance:
		return input.TargetInstanceID != nil && *input.TargetInstanceID != "" &&
			input.PerformancePercent == nil && len(input.Weights) == 0 &&
			input.BalancingMode == nil
	case ActionSetInstancePerformance:
		return input.TargetInstanceID != nil && *input.TargetInstanceID != "" &&
			input.PerformancePercent != nil && *input.PerformancePercent >= 20 &&
			*input.PerformancePercent <= 100 && len(input.Weights) == 0 &&
			input.BalancingMode == nil
	case ActionSetInstanceWeights:
		return input.TargetInstanceID == nil && input.PerformancePercent == nil &&
			len(input.Weights) > 0 && input.BalancingMode == nil
	case ActionSetBalancingMode:
		return input.TargetInstanceID == nil && input.PerformancePercent == nil &&
			len(input.Weights) == 0 && input.BalancingMode != nil &&
			(*input.BalancingMode == "fixed" || *input.BalancingMode == "adaptive")
	default:
		return false
	}
}

// Service applies lab session rules before using persistent storage.
type Service struct {
	repository createRepository
	quota      Quota
	lifetime   LifetimeConfig
	now        func() time.Time
	newID      func() (string, error)
	decorator  snapshotDecorator
}

// LifetimeConfig controls derived lab deadlines exposed in snapshots.
type LifetimeConfig struct {
	IdleTimeout time.Duration
	MaxDuration time.Duration
}

// DefaultLifetimeConfig returns the platform MVP lifecycle defaults.
func DefaultLifetimeConfig() LifetimeConfig {
	return LifetimeConfig{IdleTimeout: 10 * time.Minute, MaxDuration: 30 * time.Minute}
}

// NewService returns a MySQL-backed lab session service.
func NewService(repository *Repository, quota Quota, lifetime ...LifetimeConfig) *Service {
	return newService(repository, quota, lifetime...)
}

func newService(repository createRepository, quota Quota, lifetime ...LifetimeConfig) *Service {
	config := DefaultLifetimeConfig()
	if len(lifetime) > 0 {
		config = lifetime[0]
	}
	return &Service{
		repository: repository,
		quota:      quota,
		lifetime:   config,
		now:        time.Now,
		newID:      newLabID,
	}
}

// SetSnapshotDecorator attaches optional runtime state to persisted snapshots.
func (s *Service) SetSnapshotDecorator(decorator snapshotDecorator) {
	s.decorator = decorator
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
		ctx, userID, labID, operationID, "DESTROY_LAB",
		map[string]any{"reason": "user_requested"},
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
	session, err := s.repository.FindOwned(ctx, labID, userID)
	if err != nil {
		return Session{}, err
	}
	s.decorateDeadlines(&session)
	return session, nil
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
	snapshot, err := repository.FindSnapshot(ctx, labID, userID)
	if err != nil {
		return Snapshot{}, err
	}
	s.decorateDeadlines(&snapshot.Lab)
	s.decorateTrafficSnapshot(&snapshot)
	if s.decorator != nil {
		s.decorator.DecorateSnapshot(&snapshot)
	}
	return snapshot, nil
}

func (s *Service) decorateDeadlines(session *Session) {
	if session.StartedAt != nil {
		value := session.StartedAt.UTC().Add(s.lifetime.MaxDuration)
		session.MaximumExpiresAt = &value
	}
	if session.LastEffectiveActionAt != nil {
		value := session.LastEffectiveActionAt.UTC().Add(s.lifetime.IdleTimeout)
		session.IdleExpiresAt = &value
	}
}

func (s *Service) decorateTrafficSnapshot(snapshot *Snapshot) {
	snapshot.TrafficPolicy = TrafficPolicy{
		RequestUnits:         IntegerPolicy{Minimum: 1, Maximum: 100, Step: 1, Default: 10},
		GenerationIntervalMS: IntegerPolicy{Minimum: 250, Maximum: 5000, Step: 250, Default: 250},
	}
	snapshot.Topology.Nodes = []TopologyNode{
		{ID: "user-pool", Type: "user_pool"},
		{ID: "lab-gateway", Type: "gateway"},
	}
	snapshot.Topology.Edges = []TopologyEdge{{From: "user-pool", To: "lab-gateway"}}
	for index := range snapshot.Topology.Instances {
		instance := &snapshot.Topology.Instances[index]
		instance.CurrentLoad = 0
		instance.LoadRatio = 0
		instance.LoadState = "idle"
		instance.ObservedAt = s.now().UTC().Truncate(time.Microsecond)
		snapshot.Topology.Nodes = append(snapshot.Topology.Nodes, TopologyNode{
			ID: instance.ID, Type: "application",
		})
		snapshot.Topology.Edges = append(snapshot.Topology.Edges, TopologyEdge{
			From: "lab-gateway", To: instance.ID,
		})
	}
}
