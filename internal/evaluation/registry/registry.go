package registry

import (
	"fmt"
	"io"
	"sync"

	"pricing-catalog/pkg/connector"
)

// Factory constructs a connector using an evaluation fixture.
//
// The fixture is intentionally supplied as an io.Reader so provider-specific
// connectors can use the same pattern as the example connectors without
// changing the public Connector interface.
type Factory func(io.Reader) connector.Connector

var (
	mu        sync.RWMutex
	factories = make(map[string]Factory)
)

// Register associates a task ID with a connector factory.
//
// Registration is intended to happen from maintainer-only evaluation code.
func Register(task string, factory Factory) error {
	if task == "" {
		return fmt.Errorf("evaluation task is required")
	}
	if factory == nil {
		return fmt.Errorf("evaluation factory for %q is nil", task)
	}

	mu.Lock()
	defer mu.Unlock()

	if _, exists := factories[task]; exists {
		return fmt.Errorf("evaluation factory already registered for %q", task)
	}

	factories[task] = factory
	return nil
}

// New constructs the connector registered for task.
func New(task string, reader io.Reader) (connector.Connector, error) {
	mu.RLock()
	factory, ok := factories[task]
	mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no evaluation connector registered for task %q", task)
	}

	return factory(reader), nil
}
