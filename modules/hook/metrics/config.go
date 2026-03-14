package metrics

// Config holds the hook.metrics module configuration.
type Config struct {
	Exporter   string           `yaml:"exporter"`
	Prometheus PrometheusConfig `yaml:"prometheus"`
	Cost       CostConfig       `yaml:"cost_tracking"`
}

// PrometheusConfig holds Prometheus exporter settings.
type PrometheusConfig struct {
	Path string `yaml:"path"`
}

// CostConfig holds cost tracking settings.
type CostConfig struct {
	Enabled bool                    `yaml:"enabled"`
	Prices  map[string]ModelPricing `yaml:"prices"`
}

// ModelPricing holds per-million-token pricing for a model.
type ModelPricing struct {
	Input  float64 `yaml:"input"`
	Output float64 `yaml:"output"`
}

func (c *Config) defaults() {
	if c.Exporter == "" {
		c.Exporter = "prometheus"
	}
	if c.Prometheus.Path == "" {
		c.Prometheus.Path = "/metrics"
	}
}
