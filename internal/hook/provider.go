package hook

// Provider is implemented by modules that expose hooks for the
// message pipeline. Discovered during wiring to populate the Pipeline.
type Provider interface {
	Hooks() []Hook
}
