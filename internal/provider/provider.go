package provider

import (
	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// Provider is the interface every OSV plugin must implement.
type Provider interface {
	// Name is a unique ID, combines os. dist and arch.
	Name(dist, arch string) string

	// Init does any one-time setup: import GPG keys, register repos, etc.
	Init(dist, arch string) error

	// PreProcess does any pre-processing before the image is built, such as downloading files.
	PreProcess(template *config.ImageTemplate) error

	// BuildImage is the main function that builds the image.
	BuildImage(template *config.ImageTemplate) error

	// PostProcess does any final steps after the image is built.
	PostProcess(template *config.ImageTemplate, err error) error
}

var (
	providers = make(map[string]Provider)
)

// Register makes a Provider available under its Name().
func Register(p Provider, dist, arch string) {
	providers[p.Name(dist, arch)] = p
}

// Get returns the Provider by name.
func Get(name string) (Provider, bool) {
	p, ok := providers[name]
	return p, ok
}
