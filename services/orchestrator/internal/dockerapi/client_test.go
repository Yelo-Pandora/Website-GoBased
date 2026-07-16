package dockerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnsureContainerUsesRestrictedHostConfig(t *testing.T) {
	var createRequest containerCreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.47/containers/lab-test-app-1/json":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case "/v1.47/containers/create":
			if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"container-1"}`))
		case "/v1.47/containers/container-1/start":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected Docker path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := newClient(server.URL, "v1.47", server.Client())
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	result, err := client.EnsureContainer(context.Background(), ContainerSpec{
		Name: "lab-test-app-1", Image: "lab-app:test", User: "10001:10001",
		Labels:      map[string]string{"platform.managed": "true"},
		NetworkName: "lab-test-net", NetworkAliases: []string{"lab-test-app-1"},
		ReadOnlyRootFS: true, CapDrop: []string{"ALL"}, MemoryBytes: 128 << 20,
		NanoCPUs: 100_000_000, PidsLimit: 64,
		Healthcheck: &Healthcheck{
			Command:  []string{"/usr/local/bin/lab-app", "healthcheck"},
			Interval: 5 * time.Second, Timeout: 2 * time.Second, Retries: 10,
		},
	})
	if err != nil {
		t.Fatalf("EnsureContainer() error = %v", err)
	}
	if !result.Created || result.ID != "container-1" {
		t.Fatalf("EnsureContainer() = %#v", result)
	}
	if createRequest.HostConfig.Privileged || !createRequest.HostConfig.ReadonlyRootfs ||
		len(createRequest.HostConfig.CapDrop) != 1 ||
		createRequest.HostConfig.SecurityOpt[0] != "no-new-privileges" {
		t.Fatalf("HostConfig = %#v", createRequest.HostConfig)
	}
}

func TestEnsureNetworkRejectsUnmanagedExistingName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"network-1","Name":"lab-test-net","Labels":{}}`))
	}))
	defer server.Close()
	client, err := newClient(server.URL, "v1.47", server.Client())
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	_, err = client.EnsureNetwork(context.Background(), NetworkSpec{
		Name: "lab-test-net", Driver: "bridge", Internal: true,
		Labels: map[string]string{"platform.managed": "true"},
	})
	if err == nil {
		t.Fatal("EnsureNetwork() error = nil; want label conflict")
	}
}
