package template

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesContainerInheritance(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "resources/base.json", `{
  "templateId":"base_v1",
  "templateVersion":1,
  "resourceType":"container",
  "imageEnv":"LAB_APP_IMAGE",
  "user":"10001:10001",
  "readOnlyRootFilesystem":true,
  "privileged":false,
  "publishPorts":false,
  "capDrop":["ALL"],
  "memoryLimitMb":128,
  "pidsLimit":64,
  "modules":["health"]
}`)
	writeTemplate(t, root, "resources/child.json", `{
  "templateId":"child_v1",
  "templateVersion":1,
  "resourceType":"container",
  "extends":"base_v1",
  "modules":["health","product-read"]
}`)
	writeTemplate(t, root, "resources/network.json", `{
  "templateId":"network_v1","templateVersion":1,"resourceType":"network",
  "driver":"bridge","internal":true,"attachable":false,
  "namePattern":"lab-{labId}-net",
  "requiredLabels":{"platform.managed":"true"}
}`)
	writeTemplate(t, root, "resources/database.json", `{
  "templateId":"database_v1","templateVersion":1,"resourceType":"database",
  "databaseNamePattern":"lab_{normalizedLabId}",
  "userNamePattern":"lab_{normalizedLabId}_user",
  "provisionProcedure":"platform.provision_lab_database",
  "resetProcedure":"platform.reset_lab_database",
  "destroyProcedure":"platform.destroy_lab_database"
}`)
	writeTemplate(t, root, "resources/nginx.json", `{
  "templateId":"nginx_v1","templateVersion":1,"resourceType":"nginxFragment",
  "templatePath":"/templates/lab.conf.tmpl","outputDirectory":"/output",
  "testBeforeReload":true,"atomicReplace":true,"gracefulReload":true
}`)
	writeTemplate(t, root, "scenarios/scenario.json", `{
  "templateId":"scenario_v1","templateVersion":1,"scenarioType":"scenario",
  "resourceTemplates":{"application":"child_v1","database":"database_v1","network":"network_v1","nginxFragment":"nginx_v1"},
  "resources":{"baseCpuLimitCores":0.1,"memoryLimitMb":128,"pidsLimit":64},
  "capacity":{"baseCapacity":100,"capacityWindowMs":1000,"initialPerformancePercent":100,"minPerformancePercent":20,"maxPerformancePercent":100},
  "loadBalancing":{"initialWeight":100,"mode":"fixed"},
  "orderSimulation":{"defaultBatchSize":60,"defaultGenerationIntervalMs":1000,"processingDelayMs":0,"overloadPolicy":"drop_excess","queueMode":"disabled","concurrencyControl":"mutex","preserveArrivalOrder":false},
  "productSeeds":[]
}`)

	registry, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	container, ok := registry.Container("child_v1")
	if !ok {
		t.Fatal("child container not found")
	}
	if container.ImageEnv != "LAB_APP_IMAGE" || container.MemoryLimitMB != 128 ||
		len(container.Modules) != 2 {
		t.Fatalf("resolved container = %#v", container)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "resources/container.json", `{
  "templateId":"container_v1","templateVersion":1,"resourceType":"container",
  "imageEnv":"LAB_APP_IMAGE","user":"10001:10001",
  "readOnlyRootFilesystem":true,"privileged":false,"publishPorts":false,
  "capDrop":["ALL"],"memoryLimitMb":128,"pidsLimit":64,
  "unexpected":true
}`)
	if _, err := Load(root); err == nil {
		t.Fatal("Load() error = nil; want unknown field error")
	}
}

func writeTemplate(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
