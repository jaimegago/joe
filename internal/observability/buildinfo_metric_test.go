package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/prometheus/client_golang/prometheus"
)

// TestRegisterBuildInfo_ScrapesAsJoeBuildInfo wires the SAME prometheus exporter
// production uses (otel.go setupMetrics) and asserts the gauge renders under the
// Prometheus name joe_build_info with value 1 and the build identity carried in
// labels. This proves the dotted instrument name "joe.build.info" maps to the
// conventional underscore Prometheus name and that the labels are attached.
func TestRegisterBuildInfo_ScrapesAsJoeBuildInfo(t *testing.T) {
	reg := prometheus.NewRegistry()
	exporter, err := promexporter.New(promexporter.WithRegisterer(reg))
	if err != nil {
		t.Fatalf("prometheus exporter: %v", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	m := NewMetrics()
	unregister, err := m.RegisterBuildInfo(BuildInfo{
		Version:   "v1.2.3-test",
		Commit:    "deadbee",
		BuildTime: "2026-06-24T12:00:00Z",
		UIDigest:  "abc123",
	})
	if err != nil {
		t.Fatalf("RegisterBuildInfo: %v", err)
	}
	t.Cleanup(func() { _ = unregister() })

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	var found bool
	for _, fam := range families {
		if fam.GetName() != "joe_build_info" {
			continue
		}
		found = true
		metrics := fam.GetMetric()
		if len(metrics) != 1 {
			t.Fatalf("joe_build_info has %d series, want exactly 1 (one label-set per binary)", len(metrics))
		}
		mt := metrics[0]
		if v := mt.GetGauge().GetValue(); v != 1 {
			t.Errorf("joe_build_info value = %v, want 1", v)
		}
		labels := map[string]string{}
		for _, l := range mt.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		want := map[string]string{
			"version":    "v1.2.3-test",
			"commit":     "deadbee",
			"build_time": "2026-06-24T12:00:00Z",
			"ui_digest":  "abc123",
		}
		for k, v := range want {
			if labels[k] != v {
				t.Errorf("label %q = %q, want %q", k, labels[k], v)
			}
		}
	}
	if !found {
		t.Fatal("joe_build_info metric family not found in Prometheus scrape")
	}
}
