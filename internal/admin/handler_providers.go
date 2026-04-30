package admin

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v5"

	"gomodel/internal/core"
	"gomodel/internal/providers"
)

func (h *Handler) ProviderStatus(c *echo.Context) error {
	return c.JSON(http.StatusOK, h.buildProviderStatusResponse())
}

// RefreshRuntime handles POST /admin/api/v1/runtime/refresh
func (h *Handler) RefreshRuntime(c *echo.Context) error {
	if h.runtimeRefresher == nil {
		return handleError(c, featureUnavailableError("runtime refresh is unavailable"))
	}

	report, err := h.runtimeRefresher.RefreshRuntime(c.Request().Context())
	if err != nil {
		if gatewayErr, ok := errors.AsType[*core.GatewayError](err); ok {
			return handleError(c, gatewayErr)
		}
		return handleError(c, core.NewProviderError("runtime_refresh", http.StatusInternalServerError, "runtime refresh failed", err))
	}
	if report.Status == "" {
		report.Status = RuntimeRefreshStatusOK
	}
	if report.Steps == nil {
		report.Steps = []RuntimeRefreshStep{}
	}
	return c.JSON(http.StatusOK, report)
}

func (h *Handler) buildProviderStatusResponse() providerStatusResponse {
	configured := cloneConfiguredProviders(h.configuredProviders)
	configuredByName := make(map[string]providers.SanitizedProviderConfig, len(configured))
	nameSet := make(map[string]struct{}, len(configured))
	for _, cfg := range configured {
		name := strings.TrimSpace(cfg.Name)
		if name == "" {
			continue
		}
		configuredByName[name] = cfg
		nameSet[name] = struct{}{}
	}

	runtimeByName := make(map[string]providers.ProviderRuntimeSnapshot)
	if h.registry != nil {
		for _, snapshot := range h.registry.ProviderRuntimeSnapshots() {
			name := strings.TrimSpace(snapshot.Name)
			if name == "" {
				continue
			}
			runtimeByName[name] = snapshot
			nameSet[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	resp := providerStatusResponse{
		Summary: providerStatusSummaryResponse{
			OverallStatus: "degraded",
		},
		Providers: make([]providerStatusItemResponse, 0, len(names)),
	}

	for _, name := range names {
		cfg, hasConfig := configuredByName[name]
		runtime, hasRuntime := runtimeByName[name]
		if !hasConfig {
			cfg = providers.SanitizedProviderConfig{Name: name, Type: strings.TrimSpace(runtime.Type)}
		}
		if !hasRuntime {
			runtime = providers.ProviderRuntimeSnapshot{Name: name, Type: strings.TrimSpace(cfg.Type)}
		}
		if strings.TrimSpace(cfg.Type) == "" {
			cfg.Type = strings.TrimSpace(runtime.Type)
		}
		if strings.TrimSpace(runtime.Type) == "" {
			runtime.Type = strings.TrimSpace(cfg.Type)
		}

		status, label, reason, lastError := classifyProviderStatus(cfg, runtime)
		resp.Providers = append(resp.Providers, providerStatusItemResponse{
			Name:         name,
			Type:         strings.TrimSpace(cfg.Type),
			Status:       status,
			StatusLabel:  label,
			StatusReason: reason,
			LastError:    lastError,
			Config:       cfg,
			Runtime:      runtime,
		})
		resp.Summary.Total++
		switch status {
		case "healthy":
			resp.Summary.Healthy++
		case "unhealthy":
			resp.Summary.Unhealthy++
		default:
			resp.Summary.Degraded++
		}
	}

	switch {
	case resp.Summary.Total == 0:
		resp.Summary.OverallStatus = "degraded"
	case resp.Summary.Healthy == resp.Summary.Total:
		resp.Summary.OverallStatus = "healthy"
	case resp.Summary.Unhealthy == resp.Summary.Total:
		resp.Summary.OverallStatus = "unhealthy"
	default:
		resp.Summary.OverallStatus = "degraded"
	}

	if resp.Providers == nil {
		resp.Providers = []providerStatusItemResponse{}
	}
	return resp
}

func classifyProviderStatus(cfg providers.SanitizedProviderConfig, runtime providers.ProviderRuntimeSnapshot) (status, label, reason, lastError string) {
	modelFetchError := strings.TrimSpace(runtime.LastModelFetchError)
	availabilityError := strings.TrimSpace(runtime.LastAvailabilityError)
	configuredName := strings.TrimSpace(cfg.Name)
	usingCachedModels := runtime.Registered &&
		runtime.DiscoveredModelCount > 0 &&
		modelFetchError == "" &&
		runtime.LastModelFetchSuccessAt == nil

	lastError = modelFetchError
	if lastError == "" {
		lastError = availabilityError
	}

	switch {
	case runtime.DiscoveredModelCount > 0 && modelFetchError == "":
		if usingCachedModels {
			return "degraded", "Starting", "serving cached model inventory while live refresh finishes", lastError
		}
		return "healthy", "Healthy", "configured and model discovery succeeded", lastError
	case modelFetchError != "" && runtime.DiscoveredModelCount > 0:
		return "degraded", "Degraded", "latest model refresh failed; previous inventory is still available", lastError
	case modelFetchError != "":
		return "unhealthy", "Unhealthy", "model discovery failed and no provider models are currently available", lastError
	case availabilityError != "" && runtime.DiscoveredModelCount == 0:
		return "unhealthy", "Unhealthy", "startup availability check failed and no provider models are available", lastError
	case runtime.DiscoveredModelCount > 0:
		return "healthy", "Healthy", "provider models are currently available", lastError
	case !runtime.Registered && configuredName != "":
		return "degraded", "Starting", "provider is configured and awaiting live model discovery", lastError
	case configuredName != "":
		return "degraded", "Configured", "provider is configured but has not exposed models yet", lastError
	default:
		return "degraded", "Unknown", "provider runtime inventory is unavailable", lastError
	}
}
