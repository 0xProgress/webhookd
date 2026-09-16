package providers

// registry holds every registered provider, keyed by name.
//
// Populated by Register from provider init() functions, read by Get
// and All. The map is written only during package initialisation, before
// any request is served, and is read-only thereafter, so no
// synchronization is required.
var registry = map[string]Provider{}

// Register adds a provider to the global registry.
//
// Called from provider init() functions. Panics if a provider with the
// same name is already registered.
//
// The panic is intentional: a duplicate registration is a programming
// error that should be caught at startup, not a runtime condition to
// handle. Registering the same name twice means two packages think
// they own the same subcommand, and there is no correct way to resolve
// that at runtime.
func Register(p Provider) {
	name := p.Name()
	if _, exists := registry[name]; exists {
		panic("webhookd: provider already registered: " + name)
	}
	registry[name] = p
}

// Get retrieves a registered provider by name.
//
// The second return value is false if no provider with that name is
// registered. Get is called once per request, after the provider name
// is extracted from the URL path.
func Get(name string) (Provider, bool) {
	p, ok := registry[name]
	return p, ok
}

// All returns the names of every registered provider.
//
// Called once at startup for --list. The order of the returned slice is
// unspecified; callers that need deterministic ordering must sort it
// themselves.
func All() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
