package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"website-gobased/internal/protocol"
	"website-gobased/services/orchestrator/internal/dockerapi"
	"website-gobased/services/orchestrator/internal/nginx"
	templates "website-gobased/services/orchestrator/internal/template"
)

type dockerStub struct {
	containers []dockerapi.Resource
	networks   []dockerapi.Resource
	removed    []string
}

func (d *dockerStub) InspectImage(context.Context, string) error { return nil }

func (d *dockerStub) EnsureNetwork(
	_ context.Context,
	spec dockerapi.NetworkSpec,
) (dockerapi.EnsureResult, error) {
	d.networks = []dockerapi.Resource{{
		ID: "network-1", Name: spec.Name, Kind: "network", Labels: spec.Labels,
	}}
	return dockerapi.EnsureResult{ID: "network-1", Name: spec.Name, Created: true}, nil
}

func (*dockerStub) ConnectNetwork(context.Context, string, string, []string) error { return nil }
func (*dockerStub) DisconnectNetwork(context.Context, string, string) error        { return nil }

func (d *dockerStub) RemoveNetwork(_ context.Context, id string) error {
	d.removed = append(d.removed, id)
	return nil
}

func (d *dockerStub) EnsureContainer(
	_ context.Context,
	spec dockerapi.ContainerSpec,
) (dockerapi.EnsureResult, error) {
	resource := dockerapi.Resource{
		ID: spec.Name + "-id", Name: spec.Name, Kind: "container", Labels: spec.Labels,
	}
	d.containers = append(d.containers, resource)
	return dockerapi.EnsureResult{ID: resource.ID, Name: resource.Name, Created: true}, nil
}

func (*dockerStub) RestartContainer(context.Context, string) error          { return nil }
func (*dockerStub) UpdateContainerCPU(context.Context, string, int64) error { return nil }

func (d *dockerStub) RemoveContainer(_ context.Context, id string) error {
	d.removed = append(d.removed, id)
	return nil
}

func (*dockerStub) FindComposeContainer(
	_ context.Context,
	_ string,
	service string,
) (dockerapi.Resource, error) {
	return dockerapi.Resource{ID: service + "-id", Name: service}, nil
}

func (d *dockerStub) ListManagedContainers(
	context.Context,
	string,
) ([]dockerapi.Resource, error) {
	return append([]dockerapi.Resource(nil), d.containers...), nil
}

func (d *dockerStub) ListManagedNetworks(
	context.Context,
	string,
) ([]dockerapi.Resource, error) {
	return append([]dockerapi.Resource(nil), d.networks...), nil
}

type databaseStub struct {
	provisioned bool
	destroyed   bool
}

func (d *databaseStub) Provision(context.Context, string, string, string) error {
	d.provisioned = true
	return nil
}
func (*databaseStub) Reset(context.Context, string) error { return nil }
func (d *databaseStub) Destroy(context.Context, string, string) error {
	d.destroyed = true
	return nil
}
func (*databaseStub) List(context.Context) ([]string, error)           { return nil, nil }
func (*databaseStub) ExpectedLabIDs(context.Context) ([]string, error) { return nil, nil }

type nginxStub struct {
	failApply bool
	removed   bool
}

func (n *nginxStub) Apply(context.Context, string, []nginx.Server) error {
	if n.failApply {
		return errors.New("nginx validation failed")
	}
	return nil
}
func (n *nginxStub) Remove(context.Context, string) error {
	n.removed = true
	return nil
}
func (*nginxStub) List() ([]string, error) { return nil, nil }

func TestExecuteRejectsUnknownCommand(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := service.Execute(context.Background(), protocol.Command{
		CommandType: "RUN_SHELL", CommandID: "cmd-1", OperationID: "op-1",
		LabID: "lab-abcdef12", RequestedBy: "1",
	})
	if response.Status != "rejected" || errorCode(response.Error) != "COMMAND_NOT_ALLOWED" {
		t.Fatalf("Execute() = %#v", response)
	}
}

func TestProvisionCompensatesWhenNginxValidationFails(t *testing.T) {
	service, docker, database, nginx := newTestService(t)
	nginx.failApply = true
	response := service.Execute(context.Background(), protocol.Command{
		CommandType: commandProvisionLab,
		CommandID:   "cmd-1",
		OperationID: "op-1",
		LabID:       "lab-abcdef12",
		RequestedBy: "1",
		Payload: []byte(`{
  "courseId":3,
  "scenarioType":"application_cluster",
  "scenarioTemplateId":"application_cluster_scenario_v1",
  "initialInstances":1,
  "redisRequired":false
}`),
	})
	if response.Status != "failed" || errorCode(response.Error) != "NGINX_CONFIG_INVALID" {
		t.Fatalf("Execute() = %#v", response)
	}
	if !database.provisioned || !database.destroyed || !nginx.removed || len(docker.removed) < 2 {
		t.Fatalf(
			"compensation database=%#v nginx=%#v removed=%v",
			database,
			nginx,
			docker.removed,
		)
	}
}

func newTestService(
	t *testing.T,
) (*Service, *dockerStub, *databaseStub, *nginxStub) {
	t.Helper()
	registry, err := templates.Load(filepath.Join("..", "..", "..", "..", "configs"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	docker := &dockerStub{}
	database := &databaseStub{}
	nginx := &nginxStub{}
	service, err := NewService(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		registry,
		docker,
		database,
		nginx,
		Config{
			ComposeProject: "test", GatewayService: "lab-gateway-nginx",
			MySQLService: "shared-mysql", LabAppImage: "lab-app:test",
			RedisImage: "redis:test", DatabaseSecret: "0123456789abcdef",
		},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service, docker, database, nginx
}

func errorCode(value any) string {
	fields, ok := value.(map[string]string)
	if !ok {
		return ""
	}
	return fields["code"]
}
