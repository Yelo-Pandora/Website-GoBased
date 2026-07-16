// Package labdb calls fixed MySQL procedures for isolated lab databases.
package labdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type database interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Provisioner invokes only the allowlisted platform stored procedures.
type Provisioner struct {
	database database
}

// NewProvisioner returns a restricted lab database adapter.
func NewProvisioner(database *sql.DB) *Provisioner {
	return &Provisioner{database: database}
}

func newProvisioner(database database) *Provisioner {
	return &Provisioner{database: database}
}

// Provision idempotently creates one lab database and user.
func (p *Provisioner) Provision(
	ctx context.Context,
	databaseName string,
	userName string,
	password string,
) error {
	if !validDatabaseName(databaseName) || !validUserName(userName) || len(password) < 16 {
		return errors.New("lab database identity is invalid")
	}
	if _, err := p.database.ExecContext(
		ctx,
		"CALL platform.provision_lab_database(?, ?, ?)",
		databaseName,
		userName,
		password,
	); err != nil {
		return fmt.Errorf("provision lab database: %w", err)
	}
	return nil
}

// Reset restores one lab database to its seeded state.
func (p *Provisioner) Reset(ctx context.Context, databaseName string) error {
	if !validDatabaseName(databaseName) {
		return errors.New("lab database name is invalid")
	}
	if _, err := p.database.ExecContext(
		ctx,
		"CALL platform.reset_lab_database(?)",
		databaseName,
	); err != nil {
		return fmt.Errorf("reset lab database: %w", err)
	}
	return nil
}

// Destroy removes one lab database and its restricted user.
func (p *Provisioner) Destroy(ctx context.Context, databaseName, userName string) error {
	if !validDatabaseName(databaseName) || !validUserName(userName) {
		return errors.New("lab database identity is invalid")
	}
	if _, err := p.database.ExecContext(
		ctx,
		"CALL platform.destroy_lab_database(?, ?)",
		databaseName,
		userName,
	); err != nil {
		return fmt.Errorf("destroy lab database: %w", err)
	}
	return nil
}

func validDatabaseName(value string) bool {
	return validIdentifier(value, "lab_", "", 4, 32)
}

func validUserName(value string) bool {
	return validIdentifier(value, "lab_", "_user", 4, 23)
}

func validIdentifier(value, prefix, suffix string, minBody, maxBody int) bool {
	if len(value) <= len(prefix)+len(suffix) ||
		value[:len(prefix)] != prefix ||
		value[len(value)-len(suffix):] != suffix {
		return false
	}
	body := value[len(prefix) : len(value)-len(suffix)]
	if len(body) < minBody || len(body) > maxBody {
		return false
	}
	for _, r := range body {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
