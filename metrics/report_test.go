package metrics_test

import (
	"strings"
	"testing"
	"time"
	"ubbagent/config"
	"ubbagent/metrics"
)

func TestMetricReport_Validate(t *testing.T) {
	conf := config.Metrics{
		BufferSeconds: 10,
		Definitions: []config.MetricDefinition{
			{
				Name: "int-metric",
				Type: "int",
			},
			{
				Name: "double-metric",
				Type: "double",
			},
		},
	}

	t.Run("Valid", func(t *testing.T) {
		m := metrics.MetricReport{
			Name:      "int-metric",
			StartTime: time.Unix(0, 0),
			EndTime:   time.Unix(1, 0),
			Labels:    map[string]string{"Key": "Value"},
			Value: metrics.MetricValue{
				IntValue: 10,
			},
		}
		if err := m.Validate(conf); err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
	})

	t.Run("Unknown metric", func(t *testing.T) {
		m := metrics.MetricReport{
			Name:      "foo",
			StartTime: time.Unix(0, 0),
			EndTime:   time.Unix(1, 0),
			Labels:    map[string]string{"Key": "Value"},
			Value: metrics.MetricValue{
				IntValue: 10,
			},
		}
		if err := m.Validate(conf); err == nil || err.Error() != "Unknown metric: foo" {
			t.Fatalf("Expected error with message \"Unknown metric: foo\", got: %+v", err)
		}
	})

	t.Run("Invalid time", func(t *testing.T) {
		m := metrics.MetricReport{
			Name:      "int-metric",
			StartTime: time.Unix(10, 0),
			EndTime:   time.Unix(1, 0),
			Labels:    map[string]string{"Key": "Value"},
			Value: metrics.MetricValue{
				IntValue: 10,
			},
		}
		if err := m.Validate(conf); err == nil || !strings.Contains(err.Error(), "StartTime > EndTime") {
			t.Fatalf("Expected error containing \"StartTime > EndTime\", got: %+v", err)
		}
	})

	t.Run("Invalid type: double", func(t *testing.T) {
		m := metrics.MetricReport{
			Name:      "int-metric",
			StartTime: time.Unix(0, 0),
			EndTime:   time.Unix(1, 0),
			Labels:    map[string]string{"Key": "Value"},
			Value: metrics.MetricValue{
				DoubleValue: 10.3,
			},
		}
		if err := m.Validate(conf); err == nil || !strings.Contains(err.Error(), "double value specified") {
			t.Fatalf("Expected error containing \"double value specified\", got: %+v", err)
		}
	})

	t.Run("Invalid type: int", func(t *testing.T) {
		m := metrics.MetricReport{
			Name:      "double-metric",
			StartTime: time.Unix(0, 0),
			EndTime:   time.Unix(1, 0),
			Labels:    map[string]string{"Key": "Value"},
			Value: metrics.MetricValue{
				IntValue: 10,
			},
		}
		if err := m.Validate(conf); err == nil || !strings.Contains(err.Error(), "integer value specified") {
			t.Fatalf("Expected error containing \"integer value specified\", got: %+v", err)
		}
	})
}