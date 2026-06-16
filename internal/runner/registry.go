package runner

import (
	"fmt"
	"sort"
)

// registery -> single source of truth:
// - every project that registers itself, keyed by it's Name()
// Lowercase = package-private (only runner can see it)
var registry = map[string]Project{}

// add project to the registry
// projects will call from init()
func Register(p Project) {
	name := p.Name()

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("runner: duplicate project name %q", name))
	}
	registry[name] = p
}

// returns project registered under the name given, or false if not found
func Get(name string) (Project, bool) {
	p, ok := registry[name]
	return p, ok
}

// Names returns all registered project names, sorted alphabetically
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
