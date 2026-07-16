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
	"website-gobased/services/orchestrator/internal/labdb"
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
	provisioned    bool
	destroyed      bool
	failProvision  bool
	provisionCount int
	destroyCount   int
	managed        []labdb.ManagedDatabase
	destroyedNames []string
}

func (d *databaseStub) Provision(context.Context, string, string, string) error {
	if d.failProvision {
		return errors.New("database unavailable")
	}
	d.provisioned = true
	d.provisionCount++
	return nil
}
func (*databaseStub) Reset(context.Context, string) error { return nil }
func (d *databaseStub) Destroy(_ context.Context, databaseName string, _ string) error {
	d.destroyed = true
	d.destroyCount++
	d.destroyedNames = append(d.destroyedNames, databaseName)
	return nil
}

func (d *databaseStub) ListManaged(context.Context) ([]labdb.ManagedDatabase, error) {
	return append([]labdb.ManagedDatabase(nil), d.managed...), nil
}

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

func TestProvisionCompensatesWhenDatabaseProvisionFails(t *testing.T) {
	service, _, database, _ := newTestService(t)
	database.failProvision = true
	response := service.Execute(context.Background(), protocol.Command{
		CommandType: commandProvisionLab,
		CommandID:   "cmd-database-failure",
		OperationID: "op-database-failure",
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
	if response.Status != "failed" || errorCode(response.Error) != "LAB_DATABASE_UNAVAILABLE" {
		t.Fatalf("Execute() = %#v", response)
	}
	if database.destroyCount != 1 {
		t.Fatalf("database destroy count = %d; want 1", database.destroyCount)
	}
}

func TestBasicNamesStayWithinMySQLUserLimit(t *testing.T) {
	service, _, _, _ := newTestService(t)
	registryScenario, ok := service.registry.Scenario("application_cluster_scenario_v1")
	if !ok {
		t.Fatal("application cluster scenario is missing")
	}
	names, err := service.names("lab-abcdef0123456789abcdef0123456789", registryScenario)
	if err != nil {
		t.Fatalf("names() error = %v", err)
	}
	if len(names.databaseUser) > 32 || len(names.normalized) > 23 {
		t.Fatalf("database identities exceed limits: %#v", names)
	}
}

func TestProvisionReturnsPersistableResourceFacts(t *testing.T) {
	service, _, _, _ := newTestService(t)
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
	if response.Status != "succeeded" {
		t.Fatalf("Execute() = %#v", response)
	}
	result, ok := response.Result.(map[string]any)
	if !ok || result["networkId"] != "network-1" {
		t.Fatalf("provision result = %#v", response.Result)
	}
	instances, ok := result["instances"].([]map[string]any)
	if !ok || len(instances) != 1 || instances[0]["status"] != "running" ||
		instances[0]["effectiveCapacity"] != 100 || instances[0]["currentWeight"] != 100 {
		t.Fatalf("provision instances = %#v", result["instances"])
	}
}

func TestResetRebuildsInitialLabResources(t *testing.T) {
	service, _, database, nginx := newTestService(t)
	provision := service.Execute(context.Background(), protocol.Command{
		CommandType: commandProvisionLab,
		CommandID:   "cmd-create",
		OperationID: "op-create",
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
	if provision.Status != "succeeded" {
		t.Fatalf("provision = %#v", provision)
	}
	reset := service.Execute(context.Background(), protocol.Command{
		CommandType: commandResetLab,
		CommandID:   "cmd-reset",
		OperationID: "op-reset",
		LabID:       "lab-abcdef12",
		RequestedBy: "1",
		Payload:     []byte(`{"scenarioTemplateId":"application_cluster_scenario_v1"}`),
	})
	if reset.Status != "succeeded" {
		t.Fatalf("reset = %#v", reset)
	}
	if database.provisionCount != 2 || database.destroyCount != 1 || !nginx.removed {
		t.Fatalf("reset database=%#v nginx=%#v", database, nginx)
	}
	result, ok := reset.Result.(map[string]any)
	if !ok || result["labId"] != "lab-abcdef12" || result["networkId"] == "" {
		t.Fatalf("reset result = %#v", reset.Result)
	}
}

func TestReconcileRequiresExpectedLabIDs(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := service.Execute(context.Background(), protocol.Command{
		CommandType: commandReconcileResources,
		CommandID:   "cmd-reconcile-missing-expected",
		OperationID: "op-reconcile-missing-expected",
		LabID:       "lab-abcdef12",
		RequestedBy: "1",
		Payload:     []byte(`{"cleanup":false}`),
	})
	if response.Status != "rejected" || errorCode(response.Error) != "INVALID_COMMAND" {
		t.Fatalf("Execute() = %#v", response)
	}
}

func TestReconcileUsesExpectedLabIDs(t *testing.T) {
	service, docker, _, _ := newTestService(t)
	docker.containers = []dockerapi.Resource{{
		ID: "container-1",
		Labels: map[string]string{
			"platform.labId": "lab-abcdef12",
		},
	}}
	response := service.Execute(context.Background(), protocol.Command{
		CommandType: commandReconcileResources,
		CommandID:   "cmd-reconcile-expected",
		OperationID: "op-reconcile-expected",
		LabID:       "lab-abcdef12",
		RequestedBy: "1",
		Payload: []byte(`{
  "expectedLabIds":["lab-abcdef12"],
  "cleanup":false
}`),
	})
	if response.Status != "succeeded" {
		t.Fatalf("Execute() = %#v", response)
	}
	fields, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", response.Result)
	}
	orphans, ok := fields["orphanLabIds"].([]string)
	if !ok || len(orphans) != 0 {
		t.Fatalf("orphanLabIds = %#v", fields["orphanLabIds"])
	}
}

func TestReconcileRejectsDuplicateExpectedLabIDs(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := service.Execute(context.Background(), protocol.Command{
		CommandType: commandReconcileResources,
		CommandID:   "cmd-reconcile-duplicate",
		OperationID: "op-reconcile-duplicate",
		LabID:       "lab-abcdef12",
		RequestedBy: "1",
		Payload: []byte(`{
  "expectedLabIds":["lab-abcdef12","lab-abcdef12"],
  "cleanup":false
}`),
	})
	if response.Status != "rejected" || errorCode(response.Error) != "INVALID_COMMAND" {
		t.Fatalf("Execute() = %#v", response)
	}
}

func TestReconcileCleansOrphanDatabases(t *testing.T) {
	service, _, database, _ := newTestService(t)
	database.managed = []labdb.ManagedDatabase{
		{DatabaseName: "lab_abcdef12", UserName: "lab_abcdef12_user"},
		{DatabaseName: "lab_deadbeef", UserName: "lab_deadbeef_user"},
	}
	response := service.Execute(context.Background(), protocol.Command{
		CommandType: commandReconcileResources,
		CommandID:   "cmd-reconcile-database",
		OperationID: "op-reconcile-database",
		LabID:       "lab-abcdef12",
		RequestedBy: "1",
		Payload: []byte(`{
  "expectedLabIds":["lab-abcdef12"],
  "cleanup":true
}`),
	})
	if response.Status != "succeeded" {
		t.Fatalf("Execute() = %#v", response)
	}
	if len(database.destroyedNames) != 1 || database.destroyedNames[0] != "lab_deadbeef" {
		t.Fatalf("destroyed databases = %#v", database.destroyedNames)
	}
}

func TestDestroyAcceptsLifecycleReasonMetadata(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := service.Execute(context.Background(), protocol.Command{
		CommandType: commandDestroyLab,
		CommandID:   "cmd-destroy-lifecycle",
		OperationID: "op-destroy-lifecycle",
		LabID:       "lab-abcdef12",
		RequestedBy: "system",
		Payload:     []byte(`{"reason":"idle_timeout"}`),
	})
	if response.Status != "succeeded" {
		t.Fatalf("Execute() = %#v", response)
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
