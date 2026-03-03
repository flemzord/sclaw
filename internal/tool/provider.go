package tool

// Provider is implemented by modules that supply tools to the global
// registry. During wiring, modules implementing Provider have their
// Tools() collected and registered, replacing any built-in tool with the
// same name. This enables progressive migration from hard-coded builtins
// to configurable module-based tools.
type Provider interface {
	Tools() []Tool
}
