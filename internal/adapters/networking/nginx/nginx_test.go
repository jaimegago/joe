package nginx

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockDoer implements nginxDoer for tests.
type mockDoer struct {
	ingresses  []networkingv1.Ingress
	configMaps []corev1.ConfigMap
	statusBody string
	err        error
}

func (m *mockDoer) listIngresses(_ context.Context, namespace string, _ metav1.ListOptions) (*networkingv1.IngressList, error) {
	if m.err != nil {
		return nil, m.err
	}
	var items []networkingv1.Ingress
	for _, ing := range m.ingresses {
		if namespace == "" || ing.Namespace == namespace {
			items = append(items, ing)
		}
	}
	return &networkingv1.IngressList{Items: items}, nil
}

func (m *mockDoer) listConfigMaps(_ context.Context, namespace string, _ metav1.ListOptions) (*corev1.ConfigMapList, error) {
	if m.err != nil {
		return nil, m.err
	}
	var items []corev1.ConfigMap
	for _, cm := range m.configMaps {
		if namespace == "" || cm.Namespace == namespace {
			items = append(items, cm)
		}
	}
	return &corev1.ConfigMapList{Items: items}, nil
}

func (m *mockDoer) httpGet(_ context.Context, _ string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []byte(m.statusBody), nil
}

func pathTypePtr(pt networkingv1.PathType) *networkingv1.PathType { return &pt }
func strPtr(s string) *string                                     { return &s }

func makeIngress(name, namespace, class, host, path, svc string, port int32) networkingv1.Ingress {
	pt := networkingv1.PathTypePrefix
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: networkingv1.IngressSpec{
			IngressClassName: strPtr(class),
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     path,
									PathType: pathTypePtr(pt),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: svc,
											Port: networkingv1.ServiceBackendPort{Number: port},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestAdapter_Status_NotConnected(t *testing.T) {
	a := New()
	if a.Status().Connected {
		t.Error("expected not connected")
	}
}

func TestAdapter_ListIngresses(t *testing.T) {
	doer := &mockDoer{
		ingresses: []networkingv1.Ingress{
			makeIngress("frontend", "production", "nginx", "app.example.com", "/", "frontend-svc", 80),
			makeIngress("api", "staging", "nginx", "api.example.com", "/v1", "api-svc", 8080),
		},
	}
	a := NewWithDoer(doer, Config{})

	tests := []struct {
		name      string
		namespace string
		wantCount int
	}{
		{name: "all namespaces", namespace: "", wantCount: 2},
		{name: "production", namespace: "production", wantCount: 1},
		{name: "staging", namespace: "staging", wantCount: 1},
		{name: "missing namespace", namespace: "missing", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingresses, err := a.ListIngresses(context.Background(), tt.namespace)
			if err != nil {
				t.Fatalf("ListIngresses() error = %v", err)
			}
			if len(ingresses) != tt.wantCount {
				t.Errorf("count = %d, want %d", len(ingresses), tt.wantCount)
			}
		})
	}
}

func TestAdapter_ListIngresses_FieldMapping(t *testing.T) {
	doer := &mockDoer{
		ingresses: []networkingv1.Ingress{
			makeIngress("my-ing", "default", "nginx", "host.example.com", "/api", "my-svc", 9090),
		},
	}
	a := NewWithDoer(doer, Config{})

	result, err := a.ListIngresses(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListIngresses() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 ingress")
	}
	ing := result[0]
	if ing.Name != "my-ing" {
		t.Errorf("Name = %q, want my-ing", ing.Name)
	}
	if ing.Class != "nginx" {
		t.Errorf("Class = %q, want nginx", ing.Class)
	}
	if len(ing.Rules) != 1 || ing.Rules[0].Host != "host.example.com" {
		t.Errorf("unexpected rules: %v", ing.Rules)
	}
	if len(ing.Rules[0].Paths) != 1 || ing.Rules[0].Paths[0].ServiceName != "my-svc" {
		t.Errorf("unexpected paths: %v", ing.Rules[0].Paths)
	}
}

func TestAdapter_GetNginxStatus(t *testing.T) {
	statusBody := `Active connections: 291
server accepts handled requests
16630948 16630948 31070465
Reading: 6 Writing: 186 Waiting: 99`

	doer := &mockDoer{statusBody: statusBody}
	a := NewWithDoer(doer, Config{StatusURL: "http://nginx:8080", StatusPath: "/nginx_status"})

	status, err := a.GetNginxStatus(context.Background())
	if err != nil {
		t.Fatalf("GetNginxStatus() error = %v", err)
	}
	if status.ActiveConnections != 291 {
		t.Errorf("ActiveConnections = %d, want 291", status.ActiveConnections)
	}
	if status.Reading != 6 {
		t.Errorf("Reading = %d, want 6", status.Reading)
	}
	if status.Writing != 186 {
		t.Errorf("Writing = %d, want 186", status.Writing)
	}
	if status.Waiting != 99 {
		t.Errorf("Waiting = %d, want 99", status.Waiting)
	}
	if status.TotalRequests != 31070465 {
		t.Errorf("TotalRequests = %d, want 31070465", status.TotalRequests)
	}
}

func TestAdapter_GetNginxStatus_NoURL(t *testing.T) {
	a := NewWithDoer(&mockDoer{}, Config{})
	_, err := a.GetNginxStatus(context.Background())
	if !errors.Is(err, ErrStatusUnavailable) {
		t.Errorf("expected ErrStatusUnavailable, got %v", err)
	}
}

func TestAdapter_ListConfigMaps(t *testing.T) {
	doer := &mockDoer{
		configMaps: []corev1.ConfigMap{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "nginx-config", Namespace: "ingress-nginx"},
				Data:       map[string]string{"proxy-connect-timeout": "15"},
			},
		},
	}
	a := NewWithDoer(doer, Config{})
	cms, err := a.ListConfigMaps(context.Background(), "ingress-nginx")
	if err != nil {
		t.Fatalf("ListConfigMaps() error = %v", err)
	}
	if len(cms) != 1 {
		t.Fatalf("expected 1 configmap, got %d", len(cms))
	}
	if cms[0].Name != "nginx-config" {
		t.Errorf("Name = %q, want nginx-config", cms[0].Name)
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	a := New()
	ctx := context.Background()

	if _, err := a.ListIngresses(ctx, ""); !errors.Is(err, ErrNotConnected) {
		t.Errorf("ListIngresses: expected ErrNotConnected, got %v", err)
	}
	if _, err := a.GetNginxStatus(ctx); !errors.Is(err, ErrNotConnected) {
		t.Errorf("GetNginxStatus: expected ErrNotConnected, got %v", err)
	}
	if _, err := a.ListConfigMaps(ctx, ""); !errors.Is(err, ErrNotConnected) {
		t.Errorf("ListConfigMaps: expected ErrNotConnected, got %v", err)
	}
}

func TestAdapter_Error(t *testing.T) {
	doer := &mockDoer{err: errors.New("k8s unreachable")}
	a := NewWithDoer(doer, Config{})

	if _, err := a.ListIngresses(context.Background(), ""); err == nil {
		t.Error("expected error for list failure")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	a := NewWithDoer(&mockDoer{}, Config{})
	if err := a.Disconnect(); err != nil {
		t.Errorf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected not connected after Disconnect")
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantErr    bool
		wantStatus string
	}{
		{
			name:       "valid with status_url",
			raw:        `{"kubeconfig_path":"/home/.kube/config","status_url":"http://nginx:8080"}`,
			wantStatus: "http://nginx:8080",
		},
		{
			name: "empty config uses defaults",
			raw:  `{}`,
		},
		{
			name:    "invalid json",
			raw:     `{bad}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if cfg.StatusURL != tt.wantStatus {
					t.Errorf("StatusURL = %q, want %q", cfg.StatusURL, tt.wantStatus)
				}
				if cfg.StatusPath != "/nginx_status" {
					t.Errorf("StatusPath = %q, want /nginx_status", cfg.StatusPath)
				}
			}
		})
	}
}
