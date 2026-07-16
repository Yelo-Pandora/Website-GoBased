package labdb

import "testing"

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
