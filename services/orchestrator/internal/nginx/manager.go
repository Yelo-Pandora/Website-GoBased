// Package nginx manages trusted per-lab Nginx fragments.
package nginx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"

	"website-gobased/services/orchestrator/internal/dockerapi"
)

type dockerExecutor interface {
	FindComposeContainer(ctx context.Context, project, service string) (dockerapi.Resource, error)
	Exec(ctx context.Context, containerID string, command []string) (string, error)
}

// Server is one validated Nginx upstream server.
type Server struct {
	Host   string
	Port   int
	Weight int
}

// Config fixes the template, output path, and Compose gateway identity.
type Config struct {
	TemplatePath    string
	OutputDirectory string
	ComposeProject  string
	GatewayService  string
}

// Manager atomically updates and reloads trusted Nginx fragments.
type Manager struct {
	docker   dockerExecutor
	config   Config
	template *template.Template
	mu       sync.Mutex
}

// NewManager loads the fixed fragment template.
func NewManager(docker dockerExecutor, config Config) (*Manager, error) {
	if docker == nil || strings.TrimSpace(config.TemplatePath) == "" ||
		strings.TrimSpace(config.OutputDirectory) == "" ||
		strings.TrimSpace(config.ComposeProject) == "" ||
		strings.TrimSpace(config.GatewayService) == "" {
		return nil, errors.New("nginx manager config is incomplete")
	}
	body, err := os.ReadFile(config.TemplatePath)
	if err != nil {
		return nil, fmt.Errorf("read nginx fragment template: %w", err)
	}
	parsed, err := template.New("lab-upstream").Option("missingkey=error").Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("parse nginx fragment template: %w", err)
	}
	if err := os.MkdirAll(config.OutputDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("create nginx output directory: %w", err)
	}
	return &Manager{docker: docker, config: config, template: parsed}, nil
}

// Apply renders, validates, and reloads one per-lab fragment.
func (m *Manager) Apply(ctx context.Context, labID string, servers []Server) error {
	if !validLabID(labID) || len(servers) == 0 {
		return errors.New("nginx fragment input is invalid")
	}
	validated := append([]Server(nil), servers...)
	for _, server := range validated {
		if !validHost(server.Host) || server.Port <= 0 || server.Port > 65535 || server.Weight <= 0 {
			return errors.New("nginx upstream server is invalid")
		}
	}
	sort.Slice(validated, func(i, j int) bool {
		if validated[i].Host == validated[j].Host {
			return validated[i].Port < validated[j].Port
		}
		return validated[i].Host < validated[j].Host
	})
	var rendered bytes.Buffer
	if err := m.template.Execute(&rendered, struct {
		LabID           string
		NormalizedLabID string
		Servers         []Server
	}{
		LabID:           labID,
		NormalizedLabID: strings.ReplaceAll(labID, "-", "_"),
		Servers:         validated,
	}); err != nil {
		return fmt.Errorf("render nginx fragment: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	target := m.fragmentPath(labID)
	previous, previousErr := os.ReadFile(target)
	if previousErr != nil && !errors.Is(previousErr, os.ErrNotExist) {
		return fmt.Errorf("read previous nginx fragment: %w", previousErr)
	}
	if err := writeAtomic(target, rendered.Bytes()); err != nil {
		return err
	}
	if err := m.validateAndReload(ctx); err != nil {
		if restoreErr := restoreFile(target, previous, previousErr == nil); restoreErr != nil {
			return fmt.Errorf("apply nginx fragment: %v; restore: %w", err, restoreErr)
		}
		if previousErr == nil {
			if rollbackErr := m.validateAndReload(ctx); rollbackErr != nil {
				return fmt.Errorf(
					"apply nginx fragment: %v; reload restored config: %w",
					err,
					rollbackErr,
				)
			}
		} else if _, rollbackErr := m.validate(ctx); rollbackErr != nil {
			return fmt.Errorf(
				"apply nginx fragment: %v; validate restored config: %w",
				err,
				rollbackErr,
			)
		}
		return err
	}
	return nil
}

// Remove removes one fragment and reloads Nginx, restoring it on failure.
func (m *Manager) Remove(ctx context.Context, labID string) error {
	if !validLabID(labID) {
		return errors.New("lab id is invalid")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	target := m.fragmentPath(labID)
	previous, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read nginx fragment: %w", err)
	}
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("remove nginx fragment: %w", err)
	}
	if err := m.validateAndReload(ctx); err != nil {
		if restoreErr := writeAtomic(target, previous); restoreErr != nil {
			return fmt.Errorf("remove nginx fragment: %v; restore: %w", err, restoreErr)
		}
		return err
	}
	return nil
}

// List returns lab IDs with managed fragment files.
func (m *Manager) List() ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(m.config.OutputDirectory, "*.conf"))
	if err != nil {
		return nil, fmt.Errorf("list nginx fragments: %w", err)
	}
	result := make([]string, 0, len(paths))
	for _, item := range paths {
		labID := strings.TrimSuffix(filepath.Base(item), ".conf")
		if !validLabID(labID) {
			continue
		}
		result = append(result, labID)
	}
	sort.Strings(result)
	return result, nil
}

func (m *Manager) validateAndReload(ctx context.Context) error {
	container, err := m.gateway(ctx)
	if err != nil {
		return err
	}
	if output, err := m.docker.Exec(ctx, container.ID, []string{"nginx", "-t"}); err != nil {
		return fmt.Errorf("validate nginx config: %w: %s", err, strings.TrimSpace(output))
	}
	if output, err := m.docker.Exec(ctx, container.ID, []string{"nginx", "-s", "reload"}); err != nil {
		return fmt.Errorf("reload nginx config: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func (m *Manager) validate(ctx context.Context) (string, error) {
	container, err := m.gateway(ctx)
	if err != nil {
		return "", err
	}
	return m.docker.Exec(ctx, container.ID, []string{"nginx", "-t"})
}

func (m *Manager) gateway(ctx context.Context) (dockerapi.Resource, error) {
	container, err := m.docker.FindComposeContainer(
		ctx,
		m.config.ComposeProject,
		m.config.GatewayService,
	)
	if err != nil {
		return dockerapi.Resource{}, fmt.Errorf("find lab gateway container: %w", err)
	}
	return container, nil
}

func (m *Manager) fragmentPath(labID string) string {
	return filepath.Join(m.config.OutputDirectory, labID+".conf")
}

func writeAtomic(target string, body []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".nginx-candidate-*")
	if err != nil {
		return fmt.Errorf("create nginx candidate: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("chmod nginx candidate: %w", err)
	}
	if _, err := temporary.Write(body); err != nil {
		temporary.Close()
		return fmt.Errorf("write nginx candidate: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync nginx candidate: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close nginx candidate: %w", err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return fmt.Errorf("replace nginx fragment: %w", err)
	}
	return nil
}

func restoreFile(target string, previous []byte, existed bool) error {
	if existed {
		return writeAtomic(target, previous)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove rejected nginx fragment: %w", err)
	}
	return nil
}

func validLabID(value string) bool {
	if len(value) < 4 || len(value) > 64 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && r == '-' {
			continue
		}
		return false
	}
	return true
}

func validHost(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && (r == '-' || r == '.') {
			continue
		}
		return false
	}
	return true
}
