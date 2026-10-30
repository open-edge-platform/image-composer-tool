package oepinstaller

import (
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// Apply adds the packages and repositories of out to the merged template t.
// Call it before repository files are generated and packages resolved. Kernel
// images and meta-packages are not added; check them with CheckKernel. Kernel
// extras (headers, modules, tools) are added as packages.
func Apply(t *config.ImageTemplate, out *Output) error {
	if err := checkKernelCmdline(t.SystemConfig.Kernel.Cmdline, out.KernelCmdline); err != nil {
		return err
	}
	pkgs, err := mergePackages(t.SystemConfig.Packages, out.Packages)
	if err != nil {
		return err
	}
	repos, err := uniqueRepositories(out.Repositories)
	if err != nil {
		return err
	}
	t.SystemConfig.Packages = pkgs
	t.PackageRepositories = mergeRepositories(t.PackageRepositories, repos)
	return nil
}

// checkKernelCmdline requires every kernel-cmdline entry to be a parameter of
// the template's kernel command line.
func checkKernelCmdline(cmdline string, want []string) error {
	params := strings.Fields(cmdline)
	var missing []string
	for _, w := range want {
		if !slices.Contains(params, w) {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the OEP Installer modules need kernel parameters %v, which systemConfig.kernel.cmdline "+
			"does not set", missing)
	}
	return nil
}

// mergePackages adds the installable pkg-list entries to the template's
// packages, in ICT's "name", "name_version", and "name_version*" forms.
func mergePackages(pkgs []string, entries []Package) ([]string, error) {
	out := slices.Clone(pkgs)
	log := logger.Logger()
	for _, p := range entries {
		switch {
		case p.Kind == PackageURL:
			log.Infof("OEP Installer: %s is not in an apt repository; openedge-cli installs it on first boot", p.Entry)
			continue
		case p.Kind == PackageRemoval:
			if slices.ContainsFunc(pkgs, func(e string) bool { return packageName(e) == p.Name }) {
				return nil, fmt.Errorf("systemConfig.packages includes %q, which the OEP Installer modules require "+
					"not to be installed", p.Name)
			}
			continue
		case p.KernelRole() == KernelImage || p.KernelRole() == KernelMeta:
			continue
		}
		var err error
		if out, err = addPackage(out, ictEntry(p)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ictEntry converts an installable entry to ICT's package form.
func ictEntry(p Package) string {
	switch p.Kind {
	case PackageExactVersion:
		return p.Name + "_" + p.Version
	case PackageVersionPrefix:
		return p.Name + "_" + p.Version + "*"
	}
	return p.Name
}

// addPackage adds entry to pkgs, combining it with an entry for the same
// package when their versions are compatible.
func addPackage(pkgs []string, entry string) ([]string, error) {
	i := slices.IndexFunc(pkgs, func(e string) bool { return packageName(e) == packageName(entry) })
	if i < 0 {
		return append(pkgs, entry), nil
	}
	merged, ok := combineVersions(pkgs[i], entry)
	if !ok {
		return nil, fmt.Errorf("package %q is requested at incompatible versions: %q and %q",
			packageName(entry), pkgs[i], entry)
	}
	pkgs[i] = merged
	return pkgs, nil
}

// packageName returns the package name of an ICT package entry.
func packageName(entry string) string {
	name, _, _ := strings.Cut(entry, "_")
	return name
}

// combineVersions returns the entry that satisfies both a and b, which name the
// same package, or false when no version can.
func combineVersions(a, b string) (string, bool) {
	_, va, _ := strings.Cut(a, "_")
	_, vb, _ := strings.Cut(b, "_")
	pa, pb := strings.HasSuffix(va, "*"), strings.HasSuffix(vb, "*")
	ca, cb := strings.TrimSuffix(stripEpoch(va), "*"), strings.TrimSuffix(stripEpoch(vb), "*")
	switch {
	case va == "" || va == vb:
		return b, true
	case vb == "":
		return a, true
	case !pa && !pb:
		return "", false
	case !pa:
		return a, strings.HasPrefix(ca, cb)
	case !pb:
		return b, strings.HasPrefix(cb, ca)
	case strings.HasPrefix(ca, cb):
		return a, true
	case strings.HasPrefix(cb, ca):
		return b, true
	}
	return "", false
}

func stripEpoch(version string) string {
	if _, rest, ok := strings.Cut(version, ":"); ok {
		return rest
	}
	return version
}

// uniqueRepositories merges identical repository groups and rejects groups
// that name the same file with different content.
func uniqueRepositories(repos []Repository) ([]config.PackageRepository, error) {
	var unique []Repository
	for _, r := range repos {
		i := slices.IndexFunc(unique, func(u Repository) bool { return sharesFile(u, r) })
		if i < 0 {
			unique = append(unique, r)
			continue
		}
		if !reflect.DeepEqual(unique[i], r) {
			return nil, fmt.Errorf("the OEP Installer output names %s for two different repositories (%q and %q)",
				sharedFile(unique[i], r), unique[i].Source, r.Source)
		}
	}
	out := make([]config.PackageRepository, 0, len(unique))
	for _, r := range unique {
		out = append(out, toPackageRepository(r))
	}
	return out, nil
}

func sharesFile(a, b Repository) bool {
	return sharedFile(a, b) != ""
}

// sharedFile returns an in-image file both repositories write, if any.
func sharedFile(a, b Repository) string {
	switch {
	case a.ListFile == b.ListFile:
		return a.ListFile
	case a.KeyFile == b.KeyFile:
		return a.KeyFile
	case a.PreferencesFile != "" && a.PreferencesFile == b.PreferencesFile:
		return a.PreferencesFile
	}
	return ""
}

// toPackageRepository converts a repository group to an ICT repository whose
// apt files are written at the paths the OEP Installer uses.
func toPackageRepository(r Repository) config.PackageRepository {
	lines := []string{r.Source}
	if r.SourceSrc != "" {
		lines = append(lines, strings.TrimSpace(r.SourceSrc))
	}
	return config.PackageRepository{
		Codename:        r.Suite,
		URL:             r.URI,
		Component:       r.Component,
		PKey:            r.GPGKey,
		Priority:        originPriority(r),
		ListFile:        r.ListFile,
		SourceLines:     lines,
		KeyFile:         r.KeyFile,
		PreferencesFile: r.PreferencesFile,
		Preferences:     formatPreferences(r.Preferences),
	}
}

// formatPreferences joins apt-pref lines into an apt_preferences(5) file,
// separating stanzas, each of which starts with a Package: line.
func formatPreferences(lines []string) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 && strings.HasPrefix(line, "Package:") {
			b.WriteString("\n")
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// originPriority returns the Pin-Priority of a "Package: *" stanza that pins
// the repository's own host by origin, or 0 when there is none.
func originPriority(r Repository) int {
	u, err := url.Parse(r.URI)
	if err != nil {
		return 0
	}
	var pkg, pin string
	for _, line := range append(slices.Clone(r.Preferences), "Package:") {
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		switch key {
		case "Package":
			pkg, pin = value, ""
		case "Pin":
			pin = value
		case "Pin-Priority":
			if n, err := strconv.Atoi(value); err == nil && pkg == "*" && pin == "origin "+u.Hostname() {
				return n
			}
		}
	}
	return 0
}

// mergeRepositories appends the OEP Installer repositories, replacing any
// template repository with the same URL and suite.
func mergeRepositories(template, oep []config.PackageRepository) []config.PackageRepository {
	same := func(a, b config.PackageRepository) bool {
		return strings.TrimSuffix(a.URL, "/") == strings.TrimSuffix(b.URL, "/") && a.Codename == b.Codename
	}
	var out []config.PackageRepository
	for _, r := range template {
		if slices.ContainsFunc(oep, func(o config.PackageRepository) bool { return same(r, o) }) {
			logger.Logger().Infof("OEP Installer: using the OEP Installer's definition of repository %s %s",
				r.URL, r.Codename)
			continue
		}
		out = append(out, r)
	}
	return append(out, oep...)
}
