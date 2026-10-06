package manifest

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/version"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
)

// CompositionSchemaVersion identifies the layout of CompositionManifest.
const CompositionSchemaVersion = "1"

// CompositionManifest records the inputs an image was composed from, so a
// built artifact can be traced back to its base OS, repositories, exact
// package versions, and every additional file placed into it. It complements
// the SPDX SBOM, which covers packages only.
type CompositionManifest struct {
	SchemaVersion       string                 `json:"schemaVersion"`
	GeneratedAt         string                 `json:"generatedAt"`
	Tool                CompositionTool        `json:"tool"`
	Image               CompositionImage       `json:"image"`
	Target              CompositionTarget      `json:"target"`
	BaseRepositories    []CompositionRepo      `json:"baseRepositories"`
	PackageRepositories []CompositionRepo      `json:"packageRepositories"`
	Kernel              CompositionKernel      `json:"kernel"`
	Packages            []CompositionPackage   `json:"packages"`
	Artifacts           []CompositionArtifact  `json:"artifacts"`
	Provisioning        CompositionProvisioner `json:"provisioning"`
	SBOM                string                 `json:"sbom,omitempty"`
}

type CompositionTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type CompositionImage struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Artifact string `json:"artifact,omitempty"`
}

type CompositionTarget struct {
	OS        string `json:"os"`
	Dist      string `json:"dist"`
	Arch      string `json:"arch"`
	ImageType string `json:"imageType"`
}

type CompositionRepo struct {
	Name      string `json:"name,omitempty"`
	URL       string `json:"url"`
	Codename  string `json:"codename,omitempty"`
	Component string `json:"component,omitempty"`
	Priority  int    `json:"priority,omitempty"`
}

type CompositionKernel struct {
	Version  string   `json:"version,omitempty"`
	Packages []string `json:"packages,omitempty"`
	Cmdline  string   `json:"cmdline,omitempty"`
}

type CompositionPackage struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Arch    string `json:"arch,omitempty"`
	URL     string `json:"url,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

type CompositionArtifact struct {
	Final  string `json:"final"`
	Source string `json:"source"`
	SHA256 string `json:"sha256"`
}

type CompositionScript struct {
	Name  string `json:"name"`
	Final string `json:"final"`
	Stage string `json:"stage,omitempty"`
	Order int    `json:"order,omitempty"`
}

type CompositionProvisioner struct {
	Scripts             []CompositionScript `json:"scripts,omitempty"`
	CloudInit           bool                `json:"cloudInit"`
	ProxyHosts          []string            `json:"proxyHosts,omitempty"`
	UpgradeAllowedRepos []string            `json:"upgradeAllowedRepos,omitempty"`
	Immutable           bool                `json:"immutable"`
}

// CompositionManifestFileName returns the manifest file name for an image
// artifact base name such as "my-image-24.04".
func CompositionManifestFileName(artifactBase string) string {
	return artifactBase + ".composition.json"
}

// BuildCompositionManifest assembles the manifest from a fully resolved
// template. baseRepos are the provider repositories the build resolved
// packages from.
func BuildCompositionManifest(t *config.ImageTemplate, baseRepos []config.ProviderRepoConfig,
	artifact, sbom string) (CompositionManifest, error) {
	artifacts, err := compositionArtifacts(t.GetAdditionalFileInfo())
	if err != nil {
		return CompositionManifest{}, err
	}
	sc := t.SystemConfig
	return CompositionManifest{
		SchemaVersion: CompositionSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Tool:          CompositionTool{Name: version.Toolname, Version: version.Version},
		Image:         CompositionImage{Name: t.Image.Name, Version: t.Image.Version, Artifact: artifact},
		Target: CompositionTarget{
			OS: t.Target.OS, Dist: t.Target.Dist, Arch: t.Target.Arch, ImageType: t.Target.ImageType,
		},
		BaseRepositories:    compositionBaseRepos(baseRepos),
		PackageRepositories: compositionTemplateRepos(t.PackageRepositories),
		Kernel: CompositionKernel{
			Version: sc.Kernel.Version, Packages: t.GetKernelPackages(), Cmdline: sc.Kernel.Cmdline,
		},
		Packages:  compositionPackages(t.FullPkgListBom),
		Artifacts: artifacts,
		Provisioning: CompositionProvisioner{
			Scripts:             compositionScripts(t.SortedProvisioningScripts()),
			CloudInit:           sc.CloudInit.IsEnabled(),
			ProxyHosts:          proxyHosts(sc.Proxy),
			UpgradeAllowedRepos: sc.AptPolicy.UpgradeAllowedRepos,
			Immutable:           sc.Immutability.Enabled,
		},
		SBOM: sbom,
	}, nil
}

// WriteCompositionManifest writes the manifest as indented JSON.
func WriteCompositionManifest(m CompositionManifest, outFile string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling composition manifest: %w", err)
	}
	if err := security.SafeWriteFile(outFile, append(data, '\n'), 0644, security.RejectSymlinks); err != nil {
		return fmt.Errorf("writing composition manifest: %w", err)
	}
	log.Infof("Composition manifest written to %s", outFile)
	return nil
}

func compositionBaseRepos(repos []config.ProviderRepoConfig) []CompositionRepo {
	out := make([]CompositionRepo, 0, len(repos))
	for _, r := range repos {
		out = append(out, CompositionRepo{Name: r.Name, URL: redactURL(r.BaseURL), Component: r.Component})
	}
	return out
}

func compositionTemplateRepos(repos []config.PackageRepository) []CompositionRepo {
	out := make([]CompositionRepo, 0, len(repos))
	for _, r := range repos {
		u := redactURL(r.URL)
		if u == "" {
			u = r.Path
		}
		out = append(out, CompositionRepo{
			Name: r.ID, URL: u, Codename: r.Codename, Component: r.Component, Priority: r.Priority,
		})
	}
	return out
}

func compositionPackages(pkgs []ospackage.PackageInfo) []CompositionPackage {
	out := make([]CompositionPackage, 0, len(pkgs))
	for _, p := range pkgs {
		name := p.PkgName
		if name == "" {
			name = p.Name
		}
		cp := CompositionPackage{Name: name, Version: p.Version, Arch: p.Arch, URL: redactURL(p.URL)}
		for _, c := range p.Checksums {
			if strings.EqualFold(c.Algorithm, "sha256") {
				cp.SHA256 = c.Value
			}
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func compositionArtifacts(files []config.AdditionalFileInfo) ([]CompositionArtifact, error) {
	out := make([]CompositionArtifact, 0, len(files))
	for _, f := range files {
		sum, err := file.SHA256(f.Local)
		if err != nil {
			return nil, fmt.Errorf("hashing additional file %s: %w", f.Local, err)
		}
		out = append(out, CompositionArtifact{Final: f.Final, Source: filepath.Base(f.Local), SHA256: sum})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Final < out[j].Final })
	return out, nil
}

func compositionScripts(scripts []config.ProvisioningScript) []CompositionScript {
	out := make([]CompositionScript, 0, len(scripts))
	for _, s := range scripts {
		out = append(out, CompositionScript{Name: s.Name, Final: s.Final, Stage: s.Stage, Order: s.Order})
	}
	return out
}

// redactURL drops userinfo, query, and fragment from a URL: repository and
// package URLs may carry credentials or signed tokens, and the composition
// manifest and SPDX SBOM are world-readable build artifacts. Unparsable values,
// opaque URLs (https:user:token@host, whose userinfo lands in Opaque), and
// hostless network URLs (https:/user:token@host/p, whose userinfo lands in
// Path) are omitted.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || (u.Host == "" && u.Scheme != "" && u.Scheme != "file") {
		return ""
	}
	u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
	return u.String()
}

// proxyHosts records only proxy hosts: proxy URLs may embed credentials.
func proxyHosts(p config.ProxyConfig) []string {
	var hosts []string
	for _, raw := range []string{p.HTTPProxy, p.HTTPSProxy, p.FTPProxy} {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			hosts = append(hosts, u.Host)
		}
	}
	return hosts
}
