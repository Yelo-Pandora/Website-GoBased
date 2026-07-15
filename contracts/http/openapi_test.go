package httpcontract_test

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestPlatformOpenAPIParsesAndContainsImplementedPaths(t *testing.T) {
	data, err := os.ReadFile("platform-api.openapi.yaml")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var document struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("openapi = %q; want 3.1.0", document.OpenAPI)
	}

	wantPaths := map[string]string{
		"/api/v1/auth/login":     "post",
		"/api/v1/auth/logout":    "post",
		"/api/v1/auth/me":        "get",
		"/api/v1/courses":        "get",
		"/api/v1/courses/{slug}": "get",
		"/api/v1/labs":           "post",
	}
	for path, method := range wantPaths {
		operations, ok := document.Paths[path]
		if !ok {
			t.Errorf("path %q is missing", path)
			continue
		}
		if _, ok := operations[method]; !ok {
			t.Errorf("operation %s %s is missing", method, path)
		}
	}
	if _, ok := document.Paths["/healthz"]; ok {
		t.Error("path \"/healthz\" must stay outside the manual Swagger contract")
	}
}
