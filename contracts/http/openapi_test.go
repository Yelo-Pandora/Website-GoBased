package httpcontract_test

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

type schemaReference struct {
	Ref string `yaml:"$ref"`
}

type schemaProperty struct {
	Type string   `yaml:"type"`
	Enum []string `yaml:"enum"`
}

type schemaDefinition struct {
	OneOf      []schemaReference         `yaml:"oneOf"`
	Properties map[string]schemaProperty `yaml:"properties"`
}

type openAPIDocument struct {
	OpenAPI    string                    `yaml:"openapi"`
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]schemaDefinition `yaml:"schemas"`
	} `yaml:"components"`
}

func TestPlatformOpenAPIParsesAndContainsImplementedPaths(t *testing.T) {
	document := loadPlatformOpenAPI(t)
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

func TestPlatformOpenAPIDocumentsAllLabActions(t *testing.T) {
	document := loadPlatformOpenAPI(t)
	schemas := document.Components.Schemas
	root, ok := schemas["LabTopologyActionRequest"]
	if !ok {
		t.Fatal("LabTopologyActionRequest schema is missing")
	}
	actions := make(map[string]bool)
	for _, branch := range root.OneOf {
		name := strings.TrimPrefix(branch.Ref, "#/components/schemas/")
		if name == branch.Ref {
			t.Fatalf("lab action branch ref = %q", branch.Ref)
		}
		schema, ok := schemas[name]
		if !ok {
			t.Fatalf("lab action branch %q is missing", name)
		}
		for _, action := range schema.Properties["actionType"].Enum {
			actions[action] = true
		}
	}
	wantActions := []string{
		"ADD_INSTANCE",
		"REMOVE_INSTANCE",
		"SET_INSTANCE_PERFORMANCE",
		"SET_INSTANCE_WEIGHTS",
		"SET_BALANCING_MODE",
		"REMOVE_INSTANCE_L1",
		"ADD_INSTANCE_L1",
		"REMOVE_SESSION_REDIS",
		"ADD_SESSION_REDIS",
	}
	if len(actions) != len(wantActions) {
		t.Fatalf("lab action count = %d; want %d: %#v", len(actions), len(wantActions), actions)
	}
	for _, action := range wantActions {
		if !actions[action] {
			t.Errorf("lab action %q is missing", action)
		}
	}
	if got := schemas["LabInstanceL1ActionRequest"].Properties["targetInstanceId"].Type; got != "string" {
		t.Errorf("L1 targetInstanceId type = %q; want string", got)
	}
	if got := schemas["LabSessionRedisActionRequest"].Properties["targetInstanceId"].Type; got != "null" {
		t.Errorf("Redis targetInstanceId type = %q; want null", got)
	}
}

func loadPlatformOpenAPI(t *testing.T) openAPIDocument {
	t.Helper()
	data, err := os.ReadFile("platform-api.openapi.yaml")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var document openAPIDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return document
}
