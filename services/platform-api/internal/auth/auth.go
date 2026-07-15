// Package auth provides user authentication and server-side sessions.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials indicates that login credentials are invalid.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrAccountDisabled indicates that a user account is disabled.
	ErrAccountDisabled = errors.New("account disabled")
	// ErrInvalidSession indicates that a session is missing, expired, or revoked.
	ErrInvalidSession = errors.New("invalid session")
	// ErrRateLimited indicates that login attempts are temporarily blocked.
	ErrRateLimited = errors.New("login rate limited")
)

const (
	sessionTokenBytes = 32
	csrfDomainLabel   = "backend-learning-platform-csrf-v1"
)

var dummyPasswordHash = []byte(
	"$2b$12$WmBVqMXazj6zMhauomUF0.HtLO3A0m4YeD.NDpgPSR5.H92kB/Ccy",
)

// User is the public identity of an authenticated platform user.
type User struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Status   string `json:"status"`
}

// Session is an authenticated server-side session.
type Session struct {
	ID            uint64
	User          User
	Token         string
	CSRFTokenHash [sha256.Size]byte
	ExpiresAt     time.Time
}

// LoginResult contains the newly created session and its CSRF token.
type LoginResult struct {
	Session   Session
	CSRFToken string
}

type userRecord struct {
	User
	PasswordHash string
}

type store interface {
	FindUserByUsername(ctx context.Context, username string) (userRecord, error)
	CreateSession(
		ctx context.Context,
		userID uint64,
		tokenHash [sha256.Size]byte,
		csrfTokenHash [sha256.Size]byte,
		expiresAt time.Time,
		now time.Time,
	) (uint64, error)
	FindSession(
		ctx context.Context,
		tokenHash [sha256.Size]byte,
		now time.Time,
	) (Session, error)
	TouchSession(ctx context.Context, sessionID uint64, now time.Time) error
	RevokeSession(ctx context.Context, sessionID uint64, now time.Time) error
}

// Service authenticates credentials and manages persistent sessions.
type Service struct {
	store      store
	limiter    *Limiter
	sessionTTL time.Duration
	now        func() time.Time
}

// NewService returns an authentication service backed by MySQL.
func NewService(
	repository *Repository,
	limiter *Limiter,
	sessionTTL time.Duration,
) *Service {
	return newService(repository, limiter, sessionTTL)
}

func newService(store store, limiter *Limiter, sessionTTL time.Duration) *Service {
	return &Service{
		store:      store,
		limiter:    limiter,
		sessionTTL: sessionTTL,
		now:        time.Now,
	}
}

// Login validates credentials and creates a new persistent session.
func (s *Service) Login(
	ctx context.Context,
	username string,
	password string,
	clientKey string,
) (LoginResult, error) {
	now := s.now().UTC()
	key := loginLimitKey(username, clientKey)
	if s.limiter.Blocked(key, now) {
		return LoginResult{}, ErrRateLimited
	}

	record, err := s.store.FindUserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, errUserNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		s.limiter.Failure(key, now)
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(record.PasswordHash),
		[]byte(password),
	); err != nil {
		s.limiter.Failure(key, now)
		return LoginResult{}, ErrInvalidCredentials
	}
	if record.Status != "active" {
		return LoginResult{}, ErrAccountDisabled
	}

	token, err := randomToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate session token: %w", err)
	}
	csrfToken := deriveCSRFToken(token)
	tokenHash := sha256.Sum256([]byte(token))
	csrfTokenHash := sha256.Sum256([]byte(csrfToken))
	expiresAt := now.Add(s.sessionTTL)
	sessionID, err := s.store.CreateSession(
		ctx,
		record.ID,
		tokenHash,
		csrfTokenHash,
		expiresAt,
		now,
	)
	if err != nil {
		return LoginResult{}, err
	}

	s.limiter.Success(key)
	return LoginResult{
		Session: Session{
			ID:            sessionID,
			User:          record.User,
			Token:         token,
			CSRFTokenHash: csrfTokenHash,
			ExpiresAt:     expiresAt,
		},
		CSRFToken: csrfToken,
	}, nil
}

// Authenticate resolves a raw Session Token to a current user session.
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrInvalidSession
	}
	now := s.now().UTC()
	tokenHash := sha256.Sum256([]byte(token))
	session, err := s.store.FindSession(ctx, tokenHash, now)
	if err != nil {
		return Session{}, err
	}
	if session.User.Status != "active" {
		return Session{}, ErrInvalidSession
	}

	expectedCSRFHash := sha256.Sum256([]byte(deriveCSRFToken(token)))
	if !hmac.Equal(session.CSRFTokenHash[:], expectedCSRFHash[:]) {
		return Session{}, ErrInvalidSession
	}
	session.Token = token
	if err := s.store.TouchSession(ctx, session.ID, now); err != nil {
		return Session{}, err
	}
	return session, nil
}

// CSRFToken returns the CSRF token associated with a session.
func (s *Service) CSRFToken(session Session) string {
	return deriveCSRFToken(session.Token)
}

// ValidateCSRF checks a submitted CSRF token in constant time.
func (s *Service) ValidateCSRF(session Session, token string) bool {
	if token == "" || session.Token == "" {
		return false
	}
	expected := deriveCSRFToken(session.Token)
	return hmac.Equal([]byte(expected), []byte(token))
}

// Logout revokes a persistent session.
func (s *Service) Logout(ctx context.Context, sessionID uint64) error {
	return s.store.RevokeSession(ctx, sessionID, s.now().UTC())
}

func randomToken() (string, error) {
	data := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func deriveCSRFToken(sessionToken string) string {
	digest := hmac.New(sha256.New, []byte(sessionToken))
	digest.Write([]byte(csrfDomainLabel))
	return base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
}

func loginLimitKey(username, clientKey string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "\x00" + clientKey
}
