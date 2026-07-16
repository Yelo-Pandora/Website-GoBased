package nginx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"website-gobased/services/orchestrator/internal/dockerapi"
)

type dockerStub struct {
	commands [][]string
	failTest bool
}

func (d *dockerStub) FindComposeContainer(
	context.Context,
	string,
	string,
) (dockerapi.Resource, error) {
	return dockerapi.Resource{ID: "gateway-1"}, nil
}

func (d *dockerStub) Exec(_ context.Context, _ string, command []string) (string, error) {
	d.commands = append(d.commands, append([]string(nil), command...))
	if d.failTest && reflect.DeepEqual(command, []string{"nginx", "-t"}) {
		return "invalid", errors.New("nginx test failed")
	}
	return "ok", nil
}

func TestApplyWritesAndReloadsFragment(t *testing.T) {
	manager, docker := newTestManager(t)
	err := manager.Apply(context.Background(), "lab-test", []Server{{
		Host: "lab-test-app-1", Port: 8080, Weight: 100,
	}})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	body, err := os.ReadFile(filepath.Join(manager.config.OutputDirectory, "lab-test.conf"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(body) == 0 || len(docker.commands) != 2 {
		t.Fatalf("body=%q commands=%v", body, docker.commands)
	}
}

func TestApplyRestoresPreviousFragmentOnValidationFailure(t *testing.T) {
	manager, docker := newTestManager(t)
	target := filepath.Join(manager.config.OutputDirectory, "lab-test.conf")
	if err := os.WriteFile(target, []byte("previous"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	docker.failTest = true
	err := manager.Apply(context.Background(), "lab-test", []Server{{
		Host: "lab-test-app-1", Port: 8080, Weight: 100,
	}})
	if err == nil {
		t.Fatal("Apply() error = nil; want validation failure")
	}
	body, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(body) != "previous" {
		t.Fatalf("restored body = %q", body)
	}
}

func newTestManager(t *testing.T) (*Manager, *dockerStub) {
	t.Helper()
	root := t.TempDir()
	templatePath := filepath.Join(root, "lab.conf.tmpl")
	if err := os.WriteFile(templatePath, []byte(`upstream lab_{{.NormalizedLabID}} {
{{range .Servers}}  server {{.Host}}:{{.Port}} weight={{.Weight}};
{{end}}}
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	docker := &dockerStub{}
	manager, err := NewManager(docker, Config{
		TemplatePath: templatePath, OutputDirectory: root,
		ComposeProject: "test", GatewayService: "lab-gateway-nginx",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager, docker
}
