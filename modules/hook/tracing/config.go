package tracing

import "time"

// Config holds the hook.tracing module configuration.
type Config struct {
	Exporter string         `yaml:"exporter"`
	OTLP     OTLPConfig     `yaml:"otlp"`
	Service  ServiceConfig  `yaml:"service"`
	Sampling SamplingConfig `yaml:"sampling"`
}

// OTLPConfig holds OTLP exporter settings.
type OTLPConfig struct {
	Endpoint string            `yaml:"endpoint"`
	Protocol string            `yaml:"protocol"`
	Headers  map[string]string `yaml:"headers"`
	Insecure bool              `yaml:"insecure"`
	Timeout  time.Duration     `yaml:"timeout"`
}

// ServiceConfig holds the service identity for traces.
type ServiceConfig struct {
	Name       string            `yaml:"name"`
	Version    string            `yaml:"version"`
	Attributes map[string]string `yaml:"attributes"`
}

// SamplingConfig holds trace sampling configuration.
type SamplingConfig struct {
	Rate float64 `yaml:"rate"`
}

func (c *Config) defaults() {
	if c.Exporter == "" {
		c.Exporter = "otlp"
	}
	if c.OTLP.Endpoint == "" {
		c.OTLP.Endpoint = "localhost:4318"
	}
	if c.OTLP.Protocol == "" {
		c.OTLP.Protocol = "http"
	}
	if c.OTLP.Timeout <= 0 {
		c.OTLP.Timeout = 10 * time.Second
	}
	if c.Service.Name == "" {
		c.Service.Name = "sclaw"
	}
	if c.Sampling.Rate <= 0 || c.Sampling.Rate > 1 {
		c.Sampling.Rate = 1.0
	}
}
