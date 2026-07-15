package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var errUserNotFound = errors.New("user not found")

// Repository stores users and authentication sessions in MySQL.
type Repository struct {
	database *sql.DB
}

// NewRepository returns a MySQL-backed authentication repository.
func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

func (r *Repository) FindUserByUsername(
	ctx context.Context,
	username string,
) (userRecord, error) {
	var record userRecord
	err := r.database.QueryRowContext(ctx, `
		SELECT id, username, status, password_hash
		FROM users
		WHERE username = ?
		LIMIT 1`, username).Scan(
		&record.ID,
		&record.Username,
		&record.Status,
		&record.PasswordHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return userRecord{}, errUserNotFound
	}
	if err != nil {
		return userRecord{}, fmt.Errorf("find user by username: %w", err)
	}
	return record, nil
}

func (r *Repository) CreateSession(
	ctx context.Context,
	userID uint64,
	tokenHash [sha256.Size]byte,
	csrfTokenHash [sha256.Size]byte,
	expiresAt time.Time,
	now time.Time,
) (uint64, error) {
	result, err := r.database.ExecContext(ctx, `
		INSERT INTO auth_sessions (
			user_id,
			session_token_hash,
			csrf_token_hash,
			expires_at,
			last_seen_at
		) VALUES (?, ?, ?, ?, ?)`,
		userID,
		tokenHash[:],
		csrfTokenHash[:],
		expiresAt,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("create auth session: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read auth session id: %w", err)
	}
	return uint64(id), nil
}

func (r *Repository) FindSession(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) (Session, error) {
	var session Session
	var csrfTokenHash []byte
	err := r.database.QueryRowContext(ctx, `
		SELECT
			s.id,
			u.id,
			u.username,
			u.status,
			s.csrf_token_hash,
			s.expires_at
		FROM auth_sessions AS s
		JOIN users AS u ON u.id = s.user_id
		WHERE s.session_token_hash = ?
		  AND s.revoked_at IS NULL
		  AND s.expires_at > ?
		LIMIT 1`, tokenHash[:], now).Scan(
		&session.ID,
		&session.User.ID,
		&session.User.Username,
		&session.User.Status,
		&csrfTokenHash,
		&session.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrInvalidSession
	}
	if err != nil {
		return Session{}, fmt.Errorf("find auth session: %w", err)
	}
	if len(csrfTokenHash) != sha256.Size {
		return Session{}, fmt.Errorf("find auth session: invalid csrf token hash length")
	}
	copy(session.CSRFTokenHash[:], csrfTokenHash)
	return session, nil
}

func (r *Repository) TouchSession(
	ctx context.Context,
	sessionID uint64,
	now time.Time,
) error {
	result, err := r.database.ExecContext(ctx, `
		UPDATE auth_sessions
		SET last_seen_at = ?
		WHERE id = ?
		  AND revoked_at IS NULL
		  AND expires_at > ?`, now, sessionID, now)
	if err != nil {
		return fmt.Errorf("touch auth session: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read touched auth session rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrInvalidSession
	}
	return nil
}

func (r *Repository) RevokeSession(
	ctx context.Context,
	sessionID uint64,
	now time.Time,
) error {
	if _, err := r.database.ExecContext(ctx, `
		UPDATE auth_sessions
		SET revoked_at = ?
		WHERE id = ?
		  AND revoked_at IS NULL`, now, sessionID); err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	return nil
}
