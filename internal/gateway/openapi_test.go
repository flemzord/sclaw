package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPI_ServesValidYAML(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/openapi.yaml", nil)
	rr := httptest.NewRecorder()
	g.handleOpenAPI().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	ct := rr.Header().Get("Content-Type")
	if ct != "application/x-yaml" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/x-yaml")
	}

	// Verify the response is valid YAML.
	var spec map[string]any
	if err := yaml.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	// Check top-level keys.
	if spec["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v, want %q", spec["openapi"], "3.0.3")
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths is not a map")
	}

	// Verify essential paths exist.
	expectedPaths := []string{
		"/health",
		"/api/sessions",
		"/api/crons",
		"/api/crons/{name}",
		"/api/crons/{name}/trigger",
		"/api/openapi.yaml",
	}
	for _, p := range expectedPaths {
		if _, ok := paths[p]; !ok {
			t.Errorf("missing path %q in OpenAPI spec", p)
		}
	}
}

func TestOpenAPI_ContainsCronSchemas(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/openapi.yaml", nil)
	rr := httptest.NewRecorder()
	g.handleOpenAPI().ServeHTTP(rr, req)

	body := rr.Body.String()

	// Check that key schemas are present in the output.
	for _, schema := range []string{"CronInfo", "CronResult", "TriggerResponse"} {
		if !strings.Contains(body, schema) {
			t.Errorf("schema %q not found in OpenAPI spec", schema)
		}
	}
}
