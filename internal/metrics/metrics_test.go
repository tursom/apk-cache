package metrics

import (
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestMetricsRegistryAndRecorders(t *testing.T) {
	m := New()
	if m.Registry() == nil {
		t.Fatal("registry is nil")
	}

	m.RecordCacheHit(10)
	m.RecordCacheMiss(20)
	m.RecordResponseBytes(5)
	m.UpdateMemory(12, 100, 2)
	m.MemoryEvictions.Inc()

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if counterValue(t, families, "apk_cache_hits_total") != 1 {
		t.Fatal("cache hit counter not updated")
	}
	if counterValue(t, families, "apk_cache_misses_total") != 1 {
		t.Fatal("cache miss counter not updated")
	}
	if counterValue(t, families, "apk_cache_download_bytes_total") != 20 {
		t.Fatal("download bytes not updated")
	}
	if counterValue(t, families, "apk_cache_response_bytes_total") != 15 {
		t.Fatal("response bytes not updated")
	}
	if gaugeWithLabel(t, families, "apk_cache_memory_size_bytes", "current") != 12 {
		t.Fatal("memory current gauge not updated")
	}
	if gaugeWithLabel(t, families, "apk_cache_memory_size_bytes", "max") != 100 {
		t.Fatal("memory max gauge not updated")
	}
	if gaugeValue(t, families, "apk_cache_memory_items_total") != 2 {
		t.Fatal("memory items gauge not updated")
	}
}

func counterValue(t *testing.T, families []*dto.MetricFamily, name string) float64 {
	t.Helper()
	for _, family := range families {
		if family.GetName() == name {
			if len(family.Metric) == 0 || family.Metric[0].Counter == nil {
				t.Fatalf("counter %s missing", name)
			}
			return family.Metric[0].Counter.GetValue()
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func gaugeValue(t *testing.T, families []*dto.MetricFamily, name string) float64 {
	t.Helper()
	for _, family := range families {
		if family.GetName() == name {
			if len(family.Metric) == 0 || family.Metric[0].Gauge == nil {
				t.Fatalf("gauge %s missing", name)
			}
			return family.Metric[0].Gauge.GetValue()
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func gaugeWithLabel(t *testing.T, families []*dto.MetricFamily, name string, labelValue string) float64 {
	t.Helper()
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetValue() == labelValue && metric.Gauge != nil {
					return metric.Gauge.GetValue()
				}
			}
		}
	}
	t.Fatalf("metric %s with label %s not found; have %s", name, labelValue, metricNames(families))
	return 0
}

func metricNames(families []*dto.MetricFamily) string {
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	return strings.Join(names, ",")
}
