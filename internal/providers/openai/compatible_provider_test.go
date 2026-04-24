package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gomodel/internal/core"
	"gomodel/internal/llmclient"
)

func TestCompatibleProvider_ListModels_UsesConfiguredFallbackWhenUpstreamFailsWithHTML(t *testing.T) {
	htmlBody := `<!DOCTYPE html><html><head><title>Error</title></head><body>Not Found</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(htmlBody))
		}
	}))
	defer server.Close()

	provider := NewCompatibleProviderWithHTTPClient(
		"test-key",
		server.Client(),
		llmclient.Hooks{},
		CompatibleProviderConfig{
			ProviderName:     "opencode-go",
			BaseURL:          server.URL,
			ConfiguredModels: []string{"glm-5.1", "glm-5", "kimi-k2.5"},
		},
	)

	resp, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
		return
	}
	if len(resp.Data) != 3 {
		t.Fatalf("len(resp.Data) = %d, want 3", len(resp.Data))
	}
	expected := []string{"glm-5.1", "glm-5", "kimi-k2.5"}
	for i, id := range expected {
		if resp.Data[i].ID != id {
			t.Errorf("resp.Data[%d].ID = %q, want %q", i, resp.Data[i].ID, id)
		}
		if resp.Data[i].Object != "model" {
			t.Errorf("resp.Data[%d].Object = %q, want model", i, resp.Data[i].Object)
		}
		if resp.Data[i].OwnedBy != "opencode-go" {
			t.Errorf("resp.Data[%d].OwnedBy = %q, want opencode-go", i, resp.Data[i].OwnedBy)
		}
	}
}

func TestCompatibleProvider_ListModels_UsesConfiguredFallbackWhenUpstreamReturnsJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
		}
	}))
	defer server.Close()

	provider := NewCompatibleProviderWithHTTPClient(
		"test-key",
		server.Client(),
		llmclient.Hooks{},
		CompatibleProviderConfig{
			ProviderName:     "my-provider",
			BaseURL:          server.URL,
			ConfiguredModels: []string{"custom-model-v1"},
		},
	)

	resp, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
		return
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "custom-model-v1" {
		t.Fatalf("unexpected models: %+v", resp.Data)
	}
}

func TestCompatibleProvider_ListModels_MergesUpstreamMetadataWhenAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(core.ModelsResponse{
			Object: "list",
			Data: []core.Model{
				{ID: "shared-model", Object: "model", OwnedBy: "upstream", Created: 999},
			},
		})
	}))
	defer server.Close()

	provider := NewCompatibleProviderWithHTTPClient(
		"test-key",
		server.Client(),
		llmclient.Hooks{},
		CompatibleProviderConfig{
			ProviderName: "merged-provider",
			BaseURL:      server.URL,
			ConfiguredModels: []string{
				"shared-model",
				"only-configured",
			},
		},
	)

	resp, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(resp.Data) = %d, want 2", len(resp.Data))
	}

	// shared-model should carry upstream metadata
	var shared, onlyConfigured core.Model
	for i := range resp.Data {
		if resp.Data[i].ID == "shared-model" {
			shared = resp.Data[i]
		}
		if resp.Data[i].ID == "only-configured" {
			onlyConfigured = resp.Data[i]
		}
	}
	if shared.ID != "shared-model" || shared.Object != "model" {
		t.Errorf("shared model: id=%q, object=%q", shared.ID, shared.Object)
	}
	// Shared model keeps upstream OwnedBy since it's non-empty
	if shared.OwnedBy != "upstream" {
		t.Errorf("shared-model.OwnedBy = %q, want upstream", shared.OwnedBy)
	}

	if onlyConfigured.ID != "only-configured" {
		t.Fatalf("only-configured model: id=%q", onlyConfigured.ID)
	}
	if onlyConfigured.Object != "model" {
		t.Errorf("only-configured.Object = %q, want model", onlyConfigured.Object)
	}
	if onlyConfigured.OwnedBy != "merged-provider" {
		t.Errorf("only-configured.OwnedBy = %q, want merged-provider", onlyConfigured.OwnedBy)
	}
}

func TestCompatibleProvider_ListModels_NoConfiguredModels_OriginalBehaviorWhenUpstreamFails(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	provider := NewCompatibleProviderWithHTTPClient(
		"test-key",
		server.Client(),
		llmclient.Hooks{},
		CompatibleProviderConfig{
			ProviderName:     "test-provider",
			BaseURL:          server.URL,
			ConfiguredModels: nil,
		},
	)

	_, err := provider.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error when upstream fails and no configured models, got nil")
	}
	gatewayErr, ok := err.(*core.GatewayError)
	if !ok {
		t.Fatalf("error type = %T, want *core.GatewayError", err)
	}
	// Upstream returns 404 so error type is not_found_error; the important
	// invariant is that an error (not a fallback) is returned when no
	// configured models are present.
	if gatewayErr.Type != core.ErrorTypeProvider && gatewayErr.Type != core.ErrorTypeNotFound {
		t.Errorf("gatewayErr.Type = %q, want provider_error or not_found_error", gatewayErr.Type)
	}
}

func TestCompatibleProvider_ListModels_NoConfiguredModels_ReturnsUpstreamOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o","object":"model","owned_by":"openai"}]}`))
	}))
	defer server.Close()

	provider := NewCompatibleProviderWithHTTPClient(
		"test-key",
		server.Client(),
		llmclient.Hooks{},
		CompatibleProviderConfig{
			ProviderName:     "upstream-only",
			BaseURL:          server.URL,
			ConfiguredModels: nil,
		},
	)

	resp, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "gpt-4o" {
		t.Fatalf("unexpected models: %+v", resp.Data)
	}
}

func TestCompatibleProvider_ListModels_EmptyConfiguredModels_OriginalBehavior(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o","object":"model"}]}`))
	}))
	defer server.Close()

	provider := NewCompatibleProviderWithHTTPClient(
		"test-key",
		server.Client(),
		llmclient.Hooks{},
		CompatibleProviderConfig{
			ProviderName:     "test",
			BaseURL:          server.URL,
			ConfiguredModels: []string{}, // explicitly empty
		},
	)

	resp, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "gpt-4o" {
		t.Fatalf("unexpected models: %+v", resp.Data)
	}
}

func TestNormalizeConfiguredModels(t *testing.T) {
	got := normalizeConfiguredModels([]string{
		" glm-5.1 ",
		"",
		"glm-5",
		"glm-5.1", // duplicate
		"   ",      // whitespace only
	})

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0] != "glm-5.1" || got[1] != "glm-5" {
		t.Fatalf("got = %v, want [glm-5.1 glm-5]", got)
	}
}

func TestNormalizeConfiguredModels_AllEmpty(t *testing.T) {
	got := normalizeConfiguredModels([]string{"", "   ", ""})
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

func TestNormalizeConfiguredModels_NilInput(t *testing.T) {
	got := normalizeConfiguredModels(nil)
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}
