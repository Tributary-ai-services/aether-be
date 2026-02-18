package services

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"go.uber.org/zap"
)

// ArgoService reads Argo Events CRDs from the K8s API
type ArgoService struct {
	namespace string
	enabled   bool
	client    *http.Client
	logger    *zap.Logger
}

// ArgoEventSource represents a discovered Argo event source
type ArgoEventSource struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Type      string   `json:"type"`
	Port      int      `json:"port,omitempty"`
	Endpoints []string `json:"endpoints,omitempty"`
}

// ArgoSensor represents a discovered Argo sensor
type ArgoSensor struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// NewArgoService creates a new Argo Events service
func NewArgoService(log *logger.Logger) *ArgoService {
	namespace := os.Getenv("ARGO_EVENTS_NAMESPACE")
	if namespace == "" {
		namespace = "argo-events"
	}

	enabled := os.Getenv("ARGO_EVENTS_ENABLED") != "false"

	// Use in-cluster K8s API with service account token
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &ArgoService{
		namespace: namespace,
		enabled:   enabled,
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
		logger: log.Logger,
	}
}

// IsEnabled returns whether Argo Events integration is enabled
func (s *ArgoService) IsEnabled() bool {
	return s.enabled
}

// ListEventSources returns available Argo event sources from the cluster
func (s *ArgoService) ListEventSources(ctx context.Context) ([]ArgoEventSource, error) {
	if !s.enabled {
		return []ArgoEventSource{}, nil
	}

	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		s.logger.Debug("Not running in K8s cluster, returning empty event sources", zap.Error(err))
		return []ArgoEventSource{}, nil
	}

	url := fmt.Sprintf(
		"https://kubernetes.default.svc/apis/argoproj.io/v1alpha1/namespaces/%s/eventsources",
		s.namespace,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+string(token))

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Warn("Failed to reach K8s API for Argo event sources", zap.Error(err))
		return []ArgoEventSource{}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		s.logger.Debug("Argo EventSource CRD not available", zap.Int("status", resp.StatusCode))
		return []ArgoEventSource{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.logger.Warn("Unexpected K8s API response", zap.Int("status", resp.StatusCode), zap.String("body", string(body)))
		return []ArgoEventSource{}, nil
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Spec map[string]any `json:"spec"`
		} `json:"items"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	sources := make([]ArgoEventSource, 0, len(result.Items))
	for _, item := range result.Items {
		es := ArgoEventSource{
			Name:      item.Metadata.Name,
			Namespace: item.Metadata.Namespace,
		}

		// Detect event source type from spec keys
		for key := range item.Spec {
			switch key {
			case "webhook":
				es.Type = "webhook"
				es.Port = 12000
				es.Endpoints = extractWebhookEndpoints(item.Spec[key])
			case "calendar":
				es.Type = "calendar"
			case "resource":
				es.Type = "resource"
			case "kafka":
				es.Type = "kafka"
			case "sns", "sqs":
				es.Type = key
			case "github":
				es.Type = "github"
			case "gitlab":
				es.Type = "gitlab"
			case "generic":
				es.Type = "generic"
			}
			if es.Type != "" {
				break
			}
		}
		if es.Type == "" {
			es.Type = "unknown"
		}

		sources = append(sources, es)
	}

	return sources, nil
}

// ListSensors returns available Argo sensors from the cluster
func (s *ArgoService) ListSensors(ctx context.Context) ([]ArgoSensor, error) {
	if !s.enabled {
		return []ArgoSensor{}, nil
	}

	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		s.logger.Debug("Not running in K8s cluster, returning empty sensors", zap.Error(err))
		return []ArgoSensor{}, nil
	}

	url := fmt.Sprintf(
		"https://kubernetes.default.svc/apis/argoproj.io/v1alpha1/namespaces/%s/sensors",
		s.namespace,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+string(token))

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Warn("Failed to reach K8s API for Argo sensors", zap.Error(err))
		return []ArgoSensor{}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return []ArgoSensor{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return []ArgoSensor{}, nil
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		} `json:"items"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	sensors := make([]ArgoSensor, 0, len(result.Items))
	for _, item := range result.Items {
		sensors = append(sensors, ArgoSensor{
			Name:      item.Metadata.Name,
			Namespace: item.Metadata.Namespace,
		})
	}

	return sensors, nil
}

// extractWebhookEndpoints extracts endpoint names from a webhook spec
func extractWebhookEndpoints(webhookSpec any) []string {
	var endpoints []string
	if m, ok := webhookSpec.(map[string]any); ok {
		for name := range m {
			endpoints = append(endpoints, name)
		}
	}
	return endpoints
}
