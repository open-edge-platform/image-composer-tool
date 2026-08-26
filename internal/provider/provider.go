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

// OverlayCapable is implemented by providers that route overlay-mode templates
// (baseline.mode: overlay) through the overlay pipeline in all three phases.
//
// Overlay mode is not a no-op for a provider that ignores it: template merging
// strips the create-mode default package set from an overlay template (the
// baseline is expected to already provide it), so a provider without an overlay
// branch would build a create-mode image from a crippled package list instead of
// layering onto the baseline. Declaring the capability explicitly lets the build
// entry point reject that combination up front rather than emit a broken image.
type OverlayCapable interface {
	Provider

	// SupportsOverlay reports whether this provider implements the overlay-mode
	// pipeline for the given target.
	SupportsOverlay(dist, arch string) bool
}

// SupportsOverlay reports whether p can build overlay-mode templates for the
// given target. Providers that do not implement OverlayCapable are create-only.
func SupportsOverlay(p Provider, dist, arch string) bool {
	capable, ok := p.(OverlayCapable)
	return ok && capable.SupportsOverlay(dist, arch)
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
