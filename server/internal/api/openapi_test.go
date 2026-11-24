package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIDocumentsEveryRoute(t *testing.T) {
	var doc struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPISpec(), &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Errorf("openapi = %q, want 3.1.x", doc.OpenAPI)
	}

	for _, pattern := range RoutePatterns() {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok {
			t.Fatalf("bad route pattern %q", pattern)
		}
		path = strings.ReplaceAll(path, "{path...}", "{path}")
		methods, exists := doc.Paths[path]
		if !exists {
			t.Errorf("route %s is missing from openapi.json", pattern)
			continue
		}
		if _, exists := methods[strings.ToLower(method)]; !exists {
			t.Errorf("route %s is missing from openapi.json", pattern)
		}
	}

	for path := range doc.Paths {
		if !strings.HasPrefix(path, "/api/v1/") {
			t.Errorf("documented path %q is not under /api/v1", path)
		}
	}
}
