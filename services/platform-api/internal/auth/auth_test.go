package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type storeStub struct {
	user             userRecord
	userErr          error
	createdSessionID uint64
	createdTokenHash [sha256.Size]byte
	createdCSRFHash  [sha256.Size]byte
	createdExpiresAt time.Time
	session          Session
	sessionErr       error
	touchedSessionID uint64
	revokedSessionID uint64
}

func (s *storeStub) FindUserByUsername(
	_ context.Context,
	_ string,
) (userRecord, error) {
	return s.user, s.userErr
}

func (s *storeStub) CreateSession(
	_ context.Context,
	_ uint64,
	tokenHash [sha256.Size]byte,
	csrfTokenHash [sha256.Size]byte,
	expiresAt time.Time,
	_ time.Time,
) (uint64, error) {
	s.createdTokenHash = tokenHash
	s.createdCSRFHash = csrfTokenHash
	s.createdExpiresAt = expiresAt
	return s.createdSessionID, nil
}

func (s *storeStub) FindSession(
	_ context.Context,
	_ [sha256.Size]byte,
	_ time.Time,
) (Session, error) {
	return s.session, s.sessionErr
}

func (s *storeStub) TouchSession(
	_ context.Context,
	sessionID uint64,
	_ time.Time,
) error {
	s.touchedSessionID = sessionID
	return nil
}

func (s *storeStub) RevokeSession(
	_ context.Context,
	sessionID uint64,
	_ time.Time,
) error {
	s.revokedSessionID = sessionID
	return nil
}

func TestLoginCreatesSession(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	now := time.Date(2026, time.July, 15, 8, 0, 0, 0, time.UTC)
	store := &storeStub{
		user: userRecord{
			User:         User{ID: 7, Username: "learner", Status: "active"},
			PasswordHash: string(passwordHash),
		},
		createdSessionID: 11,
	}
	service := newService(
		store,
		NewLimiter(time.Minute, 5, time.Minute, 10),
		8*time.Hour,
	)
	service.now = func() time.Time { return now }

	result, err := service.Login(
		context.Background(),
		"learner",
		"password",
		"127.0.0.1",
	)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.Session.ID != 11 || result.Session.User.ID != 7 {
		t.Fatalf("Login() session = %#v", result.Session)
	}
	if result.Session.Token == "" || result.CSRFToken == "" {
		t.Fatal("Login() returned empty tokens")
	}
	if store.createdTokenHash == ([sha256.Size]byte{}) ||
		store.createdCSRFHash == ([sha256.Size]byte{}) {
		t.Fatal("Login() did not hash tokens")
	}
	if !store.createdExpiresAt.Equal(now.Add(8 * time.Hour)) {
		t.Fatalf("expiresAt = %v; want %v", store.createdExpiresAt, now.Add(8*time.Hour))
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	store := &storeStub{userErr: errUserNotFound}
	service := newService(
		store,
		NewLimiter(time.Minute, 5, time.Minute, 10),
		time.Hour,
	)
	service.now = func() time.Time { return time.Unix(1, 0).UTC() }

	_, err := service.Login(
		context.Background(),
		"missing",
		"password",
		"127.0.0.1",
	)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v; want ErrInvalidCredentials", err)
	}
}

func TestLoginRejectsDisabledAccount(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	store := &storeStub{user: userRecord{
		User:         User{ID: 7, Username: "learner", Status: "disabled"},
		PasswordHash: string(passwordHash),
	}}
	service := newService(
		store,
		NewLimiter(time.Minute, 5, time.Minute, 10),
		time.Hour,
	)

	_, err = service.Login(
		context.Background(),
		"learner",
		"password",
		"127.0.0.1",
	)
	if !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("Login() error = %v; want ErrAccountDisabled", err)
	}
}

func TestAuthenticateValidatesCSRFHashAndTouchesSession(t *testing.T) {
	token := "session-token"
	csrfHash := sha256.Sum256([]byte(deriveCSRFToken(token)))
	store := &storeStub{session: Session{
		ID:            17,
		User:          User{ID: 7, Username: "learner", Status: "active"},
		CSRFTokenHash: csrfHash,
		ExpiresAt:     time.Now().Add(time.Hour),
	}}
	service := newService(
		store,
		NewLimiter(time.Minute, 5, time.Minute, 10),
		time.Hour,
	)

	session, err := service.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if session.Token != token || store.touchedSessionID != 17 {
		t.Fatalf("Authenticate() session = %#v, touched = %d", session, store.touchedSessionID)
	}
	if !service.ValidateCSRF(session, service.CSRFToken(session)) {
		t.Fatal("ValidateCSRF() = false; want true")
	}
	if service.ValidateCSRF(session, "wrong") {
		t.Fatal("ValidateCSRF() = true for wrong token")
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	store := &storeStub{}
	service := newService(
		store,
		NewLimiter(time.Minute, 5, time.Minute, 10),
		time.Hour,
	)
	if err := service.Logout(context.Background(), 19); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if store.revokedSessionID != 19 {
		t.Fatalf("revoked session = %d; want 19", store.revokedSessionID)
	}
}
