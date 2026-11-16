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
	if len(out.PreferenceFiles) > 0 {
		return fmt.Errorf("the OEP Installer output has apt preferences without a repository (%s), which ICT does "+
			"not support yet", out.PreferenceFiles[0].File)
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

// addPackage adds entry to pkgs, combining it with every entry for the same
// package into one request when their versions are compatible.
func addPackage(pkgs []string, entry string) ([]string, error) {
	name := packageName(entry)
	merged, first := entry, -1
	out := make([]string, 0, len(pkgs)+1)
	for _, e := range pkgs {
		if packageName(e) != name {
			out = append(out, e)
			continue
		}
		var ok bool
		if merged, ok = combineVersions(e, merged); !ok {
			return nil, fmt.Errorf("package %q is requested at incompatible versions: %q and %q", name, e, entry)
		}
		if first < 0 {
			first = len(out)
			out = append(out, "")
		}
	}
	if first < 0 {
		return append(out, merged), nil
	}
	out[first] = merged
	return out, nil
}

// packageName returns the package name of an ICT package entry.
func packageName(entry string) string {
	name, _, _ := strings.Cut(entry, "_")
	return name
}

// versionRequest is the version part of an ICT package entry.
type versionRequest struct {
	epoch, version string
	prefix         bool
}

func parseVersionRequest(v string) versionRequest {
	r := versionRequest{version: v}
	if epoch, rest, ok := strings.Cut(v, ":"); ok {
		r.epoch, r.version = epoch, rest
	}
	r.version, r.prefix = strings.CutSuffix(r.version, "*")
	return r
}

func (r versionRequest) String() string {
	s := r.version
	if r.epoch != "" {
		s = r.epoch + ":" + s
	}
	if r.prefix {
		s += "*"
	}
	return s
}

// combineVersions returns the entry that satisfies both a and b, which name the
// same package, or false when no version can. An explicit epoch must match the
// other request's epoch when it has one.
func combineVersions(a, b string) (string, bool) {
	name, va, _ := strings.Cut(a, "_")
	_, vb, _ := strings.Cut(b, "_")
	switch {
	case va == "" || va == vb:
		return b, true
	case vb == "":
		return a, true
	}
	ra, rb := parseVersionRequest(va), parseVersionRequest(vb)
	if ra.epoch != "" && rb.epoch != "" && ra.epoch != rb.epoch {
		return "", false
	}
	keep := ra
	switch {
	case !ra.prefix && !rb.prefix:
		if ra.version != rb.version {
			return "", false
		}
	case !ra.prefix:
		if !strings.HasPrefix(ra.version, rb.version) {
			return "", false
		}
	case !rb.prefix:
		if !strings.HasPrefix(rb.version, ra.version) {
			return "", false
		}
		keep = rb
	case strings.HasPrefix(ra.version, rb.version):
	case strings.HasPrefix(rb.version, ra.version):
		keep = rb
	default:
		return "", false
	}
	if keep.epoch == "" {
		keep.epoch = ra.epoch + rb.epoch // at most one is set
	}
	return name + "_" + keep.String(), true
}

// uniqueRepositories drops repository groups identical to an earlier one and
// rejects groups that write the same in-image file with different content.
// Groups that share a file with the same content, such as a signing key, are
// both kept.
func uniqueRepositories(repos []Repository) ([]config.PackageRepository, error) {
	var unique []Repository
	for _, r := range repos {
		duplicate := false
		for _, u := range unique {
			if reflect.DeepEqual(u, r) {
				duplicate = true
				break
			}
			if file := conflictingFile(u, r); file != "" {
				return nil, fmt.Errorf("the OEP Installer output names %s for two different repositories (%q and %q)",
					file, u.Source, r.Source)
			}
		}
		if !duplicate {
			unique = append(unique, r)
		}
	}
	out := make([]config.PackageRepository, 0, len(unique))
	for _, r := range unique {
		out = append(out, toPackageRepository(r))
	}
	return out, nil
}

// conflictingFile returns an in-image file that both repositories write with
// different content, if any.
func conflictingFile(a, b Repository) string {
	switch {
	case a.ListFile == b.ListFile && (a.Source != b.Source || a.SourceSrc != b.SourceSrc):
		return a.ListFile
	case a.KeyFile == b.KeyFile && (a.GPGKey != b.GPGKey || a.GPGKeyFingerprint != b.GPGKeyFingerprint):
		return a.KeyFile
	case a.PreferencesFile != "" && a.PreferencesFile == b.PreferencesFile &&
		!slices.Equal(a.Preferences, b.Preferences):
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
		URL:             strings.TrimRight(r.URI, "/"), // ICT appends "/dists/...", so a trailing "/" would double
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
