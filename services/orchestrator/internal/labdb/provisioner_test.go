package labdb

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func TestValidDatabaseIdentity(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		validate func(string) bool
		want     bool
	}{
		{name: "database", value: "lab_a81f", validate: validDatabaseName, want: true},
		{name: "database injection", value: "lab_a81f`; DROP DATABASE platform", validate: validDatabaseName},
		{name: "user", value: "lab_a81f_user", validate: validUserName, want: true},
		{name: "user max length", value: "lab_12345678901234567890123_user", validate: validUserName, want: true},
		{name: "user too long", value: "lab_123456789012345678901234_user", validate: validUserName},
		{name: "user suffix", value: "lab_a81f_admin", validate: validUserName},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.validate(test.value); got != test.want {
				t.Fatalf("validate(%q) = %t; want %t", test.value, got, test.want)
			}
		})
	}
}

func TestListManagedIntegration(t *testing.T) {
	dsn := os.Getenv("ORCHESTRATOR_TEST_DSN")
	if dsn == "" {
		t.Skip("ORCHESTRATOR_TEST_DSN is not set")
	}
	database, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer database.Close()
	values, err := NewProvisioner(database).ListManaged(context.Background())
	if err != nil {
		t.Fatalf("ListManaged() error = %v", err)
	}
	for _, value := range values {
		if !validDatabaseName(value.DatabaseName) || !validUserName(value.UserName) {
			t.Fatalf("invalid managed database = %#v", value)
		}
	}
}
