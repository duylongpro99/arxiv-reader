package channels

import (
	"fmt"

	"github.com/maritime-ds/arxiv-reader/internal/config"
)

// factories holds channel constructors self-registered by Register (see
// below). A direct "case devto: return devto.New(cfg)" switch (the original
// llm.NewLLMClient shape) is NOT possible here: every channel package must
// import this package for the Channel/GeneratedContent/Category types, so
// this package importing a channel package back would be an import cycle.
// Self-registration (the same pattern database/sql uses for drivers) breaks
// the cycle — channels never imports devto/x; they import channels and
// register themselves via init().
var factories = map[string]func(cfg *config.Config) (Channel, error){}

// Register adds a channel constructor under id. Called from a channel
// package's init() (e.g. devto's), never at request time. Panics on a
// duplicate id — two packages claiming the same id is a build-time
// programming error, not a runtime condition to recover from.
func Register(id string, factory func(cfg *config.Config) (Channel, error)) {
	if _, exists := factories[id]; exists {
		panic(fmt.Sprintf("channels: Register called twice for id %q", id))
	}
	factories[id] = factory
}

// NewChannel selects a concrete Channel implementation from its config id.
// config.PublishingConfig.Channels already whitelists known ids at load time
// (see config.go), so an unregistered id here is defense-in-depth: it
// returns a descriptive error and NEVER a nil Channel, so an unknown/stale
// id (or a channel package the binary forgot to import — see cmd/server's
// blank imports) can never surface as a nil-pointer panic downstream.
func NewChannel(id string, cfg *config.Config) (Channel, error) {
	factory, ok := factories[id]
	if !ok {
		return nil, fmt.Errorf("unknown channel %q — implement the Channel interface for custom channels", id)
	}
	return factory(cfg)
}
