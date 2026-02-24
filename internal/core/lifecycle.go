package core

import (
	"context"

	"gopkg.in/yaml.v3"
)

// Configurable is implemented by modules that accept YAML configuration.
// Called after instantiation and before Provision().
// The node contains the raw YAML for this module's config section.
type Configurable interface {
	Configure(node *yaml.Node) error
}

// Provisioner is implemented by modules that need setup after instantiation.
// This is where modules should set defaults, validate raw config,
// and load sub-modules via AppContext.
type Provisioner interface {
	Provision(ctx *AppContext) error
}

// Validator is implemented by modules that can verify their configuration
// is complete and correct. Called after Provision().
// Validate should be read-only — no side effects.
type Validator interface {
	Validate() error
}

// Starter is implemented by modules that need to start background work
// (goroutines, listeners, connections). Called after all modules are
// provisioned and validated.
type Starter interface {
	Start() error
}

// Stopper is implemented by modules that need to clean up resources.
// Called during shutdown in reverse order of Start().
type Stopper interface {
	Stop(ctx context.Context) error
}

// Reloader is implemented by modules that support live configuration reload.
type Reloader interface {
	Reload(ctx *AppContext) error
}
