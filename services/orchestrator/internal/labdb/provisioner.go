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
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ManagedDatabase identifies one database created for a lab.
type ManagedDatabase struct {
	DatabaseName string
	UserName     string
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

// ListManaged returns lab databases through the restricted stored procedure.
func (p *Provisioner) ListManaged(ctx context.Context) ([]ManagedDatabase, error) {
	rows, err := p.database.QueryContext(ctx, "CALL platform.list_lab_database_resources()")
	if err != nil {
		return nil, fmt.Errorf("list lab databases: %w", err)
	}
	defer rows.Close()
	result := make([]ManagedDatabase, 0)
	for rows.Next() {
		var value ManagedDatabase
		if err := rows.Scan(&value.DatabaseName, &value.UserName); err != nil {
			return nil, fmt.Errorf("scan lab database: %w", err)
		}
		if !validDatabaseName(value.DatabaseName) || !validUserName(value.UserName) {
			return nil, errors.New("stored procedure returned an invalid lab database identity")
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lab databases: %w", err)
	}
	return result, nil
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
