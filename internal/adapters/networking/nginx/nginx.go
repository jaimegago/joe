package nginx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ErrNotConnected is returned when the adapter is used before Connect.
var ErrNotConnected = errors.New("nginx adapter not connected")

// Ingress is a simplified representation of a K8s Ingress resource.
type Ingress struct {
	Name         string            `json:"name"`
	Namespace    string            `json:"namespace"`
	Class        string            `json:"class"`
	Rules        []IngressRule     `json:"rules"`
	TLS          []IngressTLS      `json:"tls,omitempty"`
	LoadBalancer []LBIngress       `json:"load_balancer,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

// IngressRule describes a host-based routing rule.
type IngressRule struct {
	Host  string        `json:"host"`
	Paths []IngressPath `json:"paths"`
}

// IngressPath describes a single path rule.
type IngressPath struct {
	Path        string `json:"path"`
	PathType    string `json:"path_type"`
	ServiceName string `json:"service_name"`
	ServicePort string `json:"service_port"`
}

// IngressTLS describes a TLS entry in an Ingress.
type IngressTLS struct {
	Hosts      []string `json:"hosts"`
	SecretName string   `json:"secret_name"`
}

// LBIngress is a load balancer address (IP or hostname).
type LBIngress struct {
	IP       string `json:"ip,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// NginxStatus contains statistics from the NGINX status endpoint.
type NginxStatus struct {
	ActiveConnections int `json:"active_connections"`
	Reading           int `json:"reading"`
	Writing           int `json:"writing"`
	Waiting           int `json:"waiting"`
	TotalAccepts      int `json:"total_accepts"`
	TotalHandled      int `json:"total_handled"`
	TotalRequests     int `json:"total_requests"`
}

// ConfigMapSummary is a summary of a K8s ConfigMap used by NGINX.
type ConfigMapSummary struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Data      map[string]string `json:"data,omitempty"`
}

// NginxAdapter is the interface for interacting with an NGINX Ingress Controller.
type NginxAdapter interface {
	adapters.Adapter
	// ListIngresses returns Ingress resources, optionally filtered by namespace.
	ListIngresses(ctx context.Context, namespace string) ([]Ingress, error)
	// GetNginxStatus returns stats from the NGINX status endpoint.
	// Returns ErrStatusUnavailable if no status_url is configured.
	GetNginxStatus(ctx context.Context) (*NginxStatus, error)
	// ListConfigMaps returns ConfigMaps in the given namespace filtered by the
	// nginx.ingress.kubernetes.io/configuration-snippet annotation.
	ListConfigMaps(ctx context.Context, namespace string) ([]ConfigMapSummary, error)
}

// ErrStatusUnavailable is returned when no status_url is configured.
var ErrStatusUnavailable = errors.New("nginx status URL not configured")

// nginxDoer abstracts K8s list operations and HTTP GETs for testability.
type nginxDoer interface {
	listIngresses(ctx context.Context, namespace string, opts metav1.ListOptions) (*networkingv1.IngressList, error)
	listConfigMaps(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.ConfigMapList, error)
	httpGet(ctx context.Context, url string) ([]byte, error)
}

// Adapter implements NginxAdapter.
type Adapter struct {
	mu        sync.RWMutex
	cfg       Config
	doer      nginxDoer
	connected bool
}

// New returns an unconnected Adapter.
func New() *Adapter {
	return &Adapter{}
}

// NewWithDoer returns an Adapter pre-wired with doer (for tests).
func NewWithDoer(doer nginxDoer, cfg Config) *Adapter {
	return &Adapter{doer: doer, cfg: cfg, connected: true}
}

// Connect establishes K8s connectivity and optionally verifies the status URL.
func (a *Adapter) Connect(ctx context.Context, source store.Source) error {
	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return err
	}

	restCfg, err := buildRESTConfig(cfg.KubeconfigPath, cfg.Context)
	if err != nil {
		return fmt.Errorf("build k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("create k8s clientset: %w", err)
	}

	// Verify K8s connectivity.
	if _, err := clientset.ServerVersion(); err != nil {
		return fmt.Errorf("ping k8s cluster: %w", err)
	}

	httpCli := &http.Client{Timeout: 10 * time.Second}
	doer := &realDoer{clientset: clientset, httpClient: httpCli}

	a.mu.Lock()
	a.cfg = cfg
	a.doer = doer
	a.connected = true
	a.mu.Unlock()
	return nil
}

// Disconnect marks the adapter as disconnected.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	a.connected = false
	a.mu.Unlock()
	return nil
}

// Status returns the current connection state.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.connected {
		return adapters.Status{Connected: true, Message: "connected"}
	}
	return adapters.Status{Connected: false, Message: "not connected"}
}

// ListIngresses returns Ingress resources from the cluster.
func (a *Adapter) ListIngresses(ctx context.Context, namespace string) ([]Ingress, error) {
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	list, err := a.doer.listIngresses(ctx, namespace, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list ingresses: %w", err)
	}
	var out []Ingress
	for _, item := range list.Items {
		out = append(out, convertIngress(item))
	}
	return out, nil
}

// GetNginxStatus fetches the NGINX status page and parses it.
func (a *Adapter) GetNginxStatus(ctx context.Context) (*NginxStatus, error) {
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	a.mu.RLock()
	statusURL := a.cfg.StatusURL
	statusPath := a.cfg.StatusPath
	a.mu.RUnlock()

	if statusURL == "" {
		return nil, ErrStatusUnavailable
	}

	body, err := a.doer.httpGet(ctx, statusURL+statusPath)
	if err != nil {
		return nil, fmt.Errorf("http get nginx status: %w", err)
	}
	return parseNginxStatus(string(body))
}

// ListConfigMaps returns ConfigMaps in the given namespace.
// If namespace is empty, lists from all namespaces.
func (a *Adapter) ListConfigMaps(ctx context.Context, namespace string) ([]ConfigMapSummary, error) {
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	list, err := a.doer.listConfigMaps(ctx, namespace, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list configmaps: %w", err)
	}
	var out []ConfigMapSummary
	for _, cm := range list.Items {
		out = append(out, ConfigMapSummary{
			Name:      cm.Name,
			Namespace: cm.Namespace,
			Data:      cm.Data,
		})
	}
	return out, nil
}

func (a *Adapter) checkConnected() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

// ---- realDoer ----

type realDoer struct {
	clientset  *kubernetes.Clientset
	httpClient *http.Client
}

func (d *realDoer) listIngresses(ctx context.Context, namespace string, opts metav1.ListOptions) (*networkingv1.IngressList, error) {
	return d.clientset.NetworkingV1().Ingresses(namespace).List(ctx, opts)
}

func (d *realDoer) listConfigMaps(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.ConfigMapList, error) {
	return d.clientset.CoreV1().ConfigMaps(namespace).List(ctx, opts)
}

func (d *realDoer) httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// ---- helpers ----

func buildRESTConfig(kubeconfigPath, context string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		if cfg, err := rest.InClusterConfig(); err == nil {
			return cfg, nil
		}
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}
	overrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		overrides.CurrentContext = context
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

func convertIngress(item networkingv1.Ingress) Ingress {
	ing := Ingress{
		Name:        item.Name,
		Namespace:   item.Namespace,
		Annotations: item.Annotations,
	}
	if item.Spec.IngressClassName != nil {
		ing.Class = *item.Spec.IngressClassName
	}
	for _, rule := range item.Spec.Rules {
		ir := IngressRule{Host: rule.Host}
		if rule.HTTP != nil {
			for _, p := range rule.HTTP.Paths {
				pt := ""
				if p.PathType != nil {
					pt = string(*p.PathType)
				}
				port := ""
				if p.Backend.Service != nil {
					port = p.Backend.Service.Port.Name
					if port == "" {
						port = strconv.Itoa(int(p.Backend.Service.Port.Number))
					}
					ir.Paths = append(ir.Paths, IngressPath{
						Path:        p.Path,
						PathType:    pt,
						ServiceName: p.Backend.Service.Name,
						ServicePort: port,
					})
				}
			}
		}
		ing.Rules = append(ing.Rules, ir)
	}
	for _, t := range item.Spec.TLS {
		ing.TLS = append(ing.TLS, IngressTLS{
			Hosts:      t.Hosts,
			SecretName: t.SecretName,
		})
	}
	for _, lb := range item.Status.LoadBalancer.Ingress {
		ing.LoadBalancer = append(ing.LoadBalancer, LBIngress{
			IP:       lb.IP,
			Hostname: lb.Hostname,
		})
	}
	return ing
}

// parseNginxStatus parses the text format returned by the NGINX status endpoint.
//
// Example response:
//
//	Active connections: 291
//	server accepts handled requests
//	16630948 16630948 31070465
//	Reading: 6 Writing: 186 Waiting: 99
func parseNginxStatus(body string) (*NginxStatus, error) {
	s := &NginxStatus{}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Active connections:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Active connections:")))
			if err != nil {
				return nil, fmt.Errorf("parse active connections: %w", err)
			}
			s.ActiveConnections = n
		} else if i > 0 && strings.TrimSpace(lines[i-1]) == "server accepts handled requests" {
			// The numbers line: accepts handled requests
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				s.TotalAccepts, _ = strconv.Atoi(fields[0])
				s.TotalHandled, _ = strconv.Atoi(fields[1])
				s.TotalRequests, _ = strconv.Atoi(fields[2])
			}
		} else if strings.Contains(line, "Reading:") {
			parseRWW(line, s)
		}
	}
	return s, nil
}

func parseRWW(line string, s *NginxStatus) {
	// "Reading: 6 Writing: 186 Waiting: 99"
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i++ {
		val, _ := strconv.Atoi(fields[i+1])
		switch fields[i] {
		case "Reading:":
			s.Reading = val
		case "Writing:":
			s.Writing = val
		case "Waiting:":
			s.Waiting = val
		}
	}
}
