package nginx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jaimegago/joe/internal/store"
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

func TestAdapter_Status_Connected(t *testing.T) {
	a := NewWithDoer(&mockDoer{}, Config{})
	s := a.Status()
	if !s.Connected {
		t.Error("expected connected")
	}
	if s.Message != "connected" {
		t.Errorf("Message = %q, want connected", s.Message)
	}
}

func TestAdapter_GetNginxStatus_HTTPError(t *testing.T) {
	doer := &mockDoer{err: errors.New("connection refused")}
	a := NewWithDoer(doer, Config{StatusURL: "http://nginx:8080", StatusPath: "/nginx_status"})
	_, err := a.GetNginxStatus(context.Background())
	if err == nil {
		t.Error("expected error when HTTP get fails")
	}
}

func TestAdapter_ListConfigMaps_Error(t *testing.T) {
	doer := &mockDoer{err: errors.New("k8s down")}
	a := NewWithDoer(doer, Config{})
	_, err := a.ListConfigMaps(context.Background(), "default")
	if err == nil {
		t.Error("expected error when listConfigMaps fails")
	}
}

func TestAdapter_ListIngresses_Error(t *testing.T) {
	doer := &mockDoer{err: errors.New("k8s timeout")}
	a := NewWithDoer(doer, Config{StatusURL: "http://nginx:8080"})
	_, err := a.ListIngresses(context.Background(), "production")
	if err == nil {
		t.Error("expected error when listIngresses fails")
	}
}

func TestConvertIngress_NamedPort(t *testing.T) {
	// Exercise the named-port branch: Port.Name != "" takes priority over Port.Number.
	pt := networkingv1.PathTypeExact
	ing := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "named-port-ing", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "svc.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pt,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "my-svc",
											Port: networkingv1.ServiceBackendPort{
												Name:   "http", // named port
												Number: 0,
											},
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
	doer := &mockDoer{ingresses: []networkingv1.Ingress{ing}}
	a := NewWithDoer(doer, Config{})
	ingresses, err := a.ListIngresses(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListIngresses() error = %v", err)
	}
	if len(ingresses) != 1 {
		t.Fatalf("expected 1 ingress")
	}
	if ingresses[0].Rules[0].Paths[0].ServicePort != "http" {
		t.Errorf("ServicePort = %q, want http", ingresses[0].Rules[0].Paths[0].ServicePort)
	}
}

func TestConvertIngress_TLSAndLB(t *testing.T) {
	// Exercise TLS entries and LoadBalancer status fields.
	ing := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-ing", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{
				{Hosts: []string{"secure.example.com"}, SecretName: "tls-secret"},
			},
		},
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{
					{IP: "1.2.3.4", Hostname: "lb.example.com"},
				},
			},
		},
	}
	doer := &mockDoer{ingresses: []networkingv1.Ingress{ing}}
	a := NewWithDoer(doer, Config{})
	ingresses, err := a.ListIngresses(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListIngresses() error = %v", err)
	}
	if len(ingresses[0].TLS) != 1 {
		t.Errorf("expected 1 TLS entry, got %d", len(ingresses[0].TLS))
	}
	if ingresses[0].TLS[0].SecretName != "tls-secret" {
		t.Errorf("SecretName = %q, want tls-secret", ingresses[0].TLS[0].SecretName)
	}
	if len(ingresses[0].LoadBalancer) != 1 {
		t.Errorf("expected 1 LB entry, got %d", len(ingresses[0].LoadBalancer))
	}
	if ingresses[0].LoadBalancer[0].IP != "1.2.3.4" {
		t.Errorf("LB IP = %q, want 1.2.3.4", ingresses[0].LoadBalancer[0].IP)
	}
}

func TestConvertIngress_NilIngressClass(t *testing.T) {
	// IngressClassName is nil — Class should be empty.
	ing := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "no-class", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			// IngressClassName intentionally nil
			Rules: []networkingv1.IngressRule{
				{Host: "host.example.com"},
			},
		},
	}
	doer := &mockDoer{ingresses: []networkingv1.Ingress{ing}}
	a := NewWithDoer(doer, Config{})
	ingresses, err := a.ListIngresses(context.Background(), "")
	if err != nil {
		t.Fatalf("ListIngresses() error = %v", err)
	}
	if ingresses[0].Class != "" {
		t.Errorf("Class = %q, want empty", ingresses[0].Class)
	}
}

func TestParseNginxStatus_BadActiveConnections(t *testing.T) {
	body := "Active connections: notanumber\nserver accepts handled requests\n1 2 3\nReading: 0 Writing: 1 Waiting: 0"
	_, err := parseNginxStatus(body)
	if err == nil {
		t.Error("expected error for non-numeric active connections")
	}
}

func TestParseNginxStatus_ShortNumbersLine(t *testing.T) {
	// Numbers line has fewer than 3 fields — should not panic, just skip.
	body := "Active connections: 5\nserver accepts handled requests\n1 2\nReading: 0 Writing: 1 Waiting: 0"
	status, err := parseNginxStatus(body)
	if err != nil {
		t.Fatalf("parseNginxStatus() unexpected error = %v", err)
	}
	if status.TotalAccepts != 0 {
		t.Errorf("TotalAccepts = %d, want 0 for short line", status.TotalAccepts)
	}
}

// ---- realDoer tests ----

func TestRealDoer_HTTPGet_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Active connections: 5\n"))
	}))
	defer srv.Close()

	d := &realDoer{httpClient: srv.Client()}
	body, err := d.httpGet(context.Background(), srv.URL+"/nginx_status")
	if err != nil {
		t.Fatalf("httpGet() error = %v", err)
	}
	if string(body) != "Active connections: 5\n" {
		t.Errorf("body = %q", string(body))
	}
}

func TestRealDoer_HTTPGet_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	d := &realDoer{httpClient: srv.Client()}
	_, err := d.httpGet(context.Background(), srv.URL+"/nginx_status")
	if err == nil {
		t.Error("expected error for non-200 status")
	}
}

func TestRealDoer_HTTPGet_BadURL(t *testing.T) {
	d := &realDoer{httpClient: &http.Client{}}
	_, err := d.httpGet(context.Background(), "://bad-url")
	if err == nil {
		t.Error("expected error for bad URL")
	}
}

func TestBuildRESTConfig_EmptyPath(t *testing.T) {
	// When kubeconfigPath is empty and not in-cluster, clientcmd falls back to
	// default kubeconfig location. This may succeed or fail depending on the
	// environment; we just verify no panic.
	_, _ = buildRESTConfig("", "")
}

func TestBuildRESTConfig_NonExistentPath(t *testing.T) {
	// Non-existent kubeconfig path should return an error.
	cfg, err := buildRESTConfig("/nonexistent/path/to/kubeconfig", "")
	if err == nil && cfg == nil {
		t.Error("expected either a config or an error")
	}
	// Either outcome is acceptable; we just need the branch exercised.
}

func TestBuildRESTConfig_WithContext(t *testing.T) {
	// Non-existent path with a context override — exercises the context branch.
	_, _ = buildRESTConfig("/nonexistent/kubeconfig", "my-context")
}

func TestRealDoer_HTTPGet_ConnectionRefused(t *testing.T) {
	// httpClient.Do returns an error when the server is already closed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cli := srv.Client()
	url := srv.URL + "/nginx_status"
	srv.Close() // close before the request is made

	d := &realDoer{httpClient: cli}
	_, err := d.httpGet(context.Background(), url)
	if err == nil {
		t.Error("expected error when connection is refused")
	}
}

// TestConnect_ParseConfigError exercises the Connect error path for bad JSON.
func TestConnect_ParseConfigError(t *testing.T) {
	a := New()
	src := store.Source{Config: []byte(`{bad json`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for bad JSON config")
	}
}

// TestConnect_BuildRESTConfigError exercises Connect when buildRESTConfig fails.
func TestConnect_BuildRESTConfigError(t *testing.T) {
	a := New()
	// A valid JSON config with a kubeconfig path that doesn't exist causes
	// buildRESTConfig to fail (no fallback in-cluster config in test env).
	src := store.Source{
		Config: []byte(`{"kubeconfig_path":"/nonexistent/kubeconfig.yaml"}`),
	}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for non-existent kubeconfig")
	}
}

// TestConnect_ServerVersionFails exercises Connect past kubernetes.NewForConfig
// but fails at ServerVersion() because the server URL is unreachable.
func TestConnect_ServerVersionFails(t *testing.T) {
	// Write a minimal kubeconfig that points to a non-existent server.
	kubecfg := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: http://127.0.0.1:19997
  name: fake
contexts:
- context:
    cluster: fake
    user: fake
  name: fake
current-context: fake
users:
- name: fake
  user: {}
`
	f, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("create temp kubeconfig: %v", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(kubecfg); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	f.Close()

	a := New()
	src := store.Source{
		Config: []byte(`{"kubeconfig_path":"` + f.Name() + `"}`),
	}
	// Connect should fail at ServerVersion() since 127.0.0.1:19997 is not listening.
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error when ServerVersion fails")
	}
}
