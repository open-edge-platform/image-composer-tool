package oepinstaller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
)

// newRepository builds a Repository from a component document with an
// apt-source line, checking what the schema cannot express.
func newRepository(doc document) (Repository, error) {
	repo := Repository{
		Name:              doc.RepoName,
		GPGKey:            doc.GPGKey,
		GPGKeyFingerprint: doc.GPGKeyFingerprint,
		KeyFile:           doc.KeyFile,
		Source:            doc.AptSource,
		SourceSrc:         doc.AptSrcSource,
		ListFile:          doc.AptListFile,
		Preferences:       doc.AptPref,
		PreferencesFile:   doc.AptPrefFile,
	}
	opts, fields, err := splitAptSource(doc.AptSource)
	if err != nil {
		return Repository{}, err
	}
	if len(fields) != 3 {
		return Repository{}, fmt.Errorf("apt-source %q: want exactly one URI, suite, and component", doc.AptSource)
	}
	repo.URI, repo.Suite, repo.Component = fields[0], fields[1], fields[2]
	if signedBy := opts["signed-by"]; signedBy != doc.KeyFile {
		return Repository{}, fmt.Errorf("apt-source %q: signed-by %q must equal key-file %q",
			doc.AptSource, signedBy, doc.KeyFile)
	}
	if arch := opts["arch"]; arch != "" {
		repo.Architectures = strings.Split(arch, ",")
	}
	if repo.Pins, err = parsePins(doc.AptPref); err != nil {
		return Repository{}, fmt.Errorf("apt-pref: %w", err)
	}
	return repo, nil
}

// parsePins parses apt-pref lines, each stanza starting at a Package: line,
// into pins. Only "Pin: origin <host>" and "Pin: version <glob>" are
// supported; any other form is rejected rather than ignored.
func parsePins(lines []string) ([]debutils.Pin, error) {
	var pins []debutils.Pin
	for _, line := range lines {
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if key == "Package" {
			pins = append(pins, debutils.Pin{Packages: strings.Fields(value)})
			continue
		}
		if len(pins) == 0 {
			return nil, fmt.Errorf("%q comes before the first Package: line", line)
		}
		p := &pins[len(pins)-1]
		if err := setPinField(p, key, value); err != nil {
			return nil, err
		}
	}
	for _, p := range pins {
		if len(p.Packages) == 0 || (p.Origin == "" && p.Version == "") {
			return nil, fmt.Errorf("stanza for %v needs a Package, a Pin, and a Pin-Priority", p.Packages)
		}
	}
	return pins, nil
}

// setPinField applies one "key: value" line of a stanza.
func setPinField(p *debutils.Pin, key, value string) error {
	switch key {
	case "Pin":
		kind, arg, _ := strings.Cut(value, " ")
		arg = strings.TrimSpace(arg)
		switch {
		case kind == "origin" && arg != "":
			p.Origin = strings.Trim(arg, `"`)
		case kind == "version" && arg != "":
			p.Version = arg
		default:
			return fmt.Errorf("unsupported %q; only origin and version pins are supported", "Pin: "+value)
		}
	case "Pin-Priority":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid Pin-Priority %q", value)
		}
		p.Priority = n
	case "Explanation":
	default:
		return fmt.Errorf("unsupported apt preferences field %q", key)
	}
	return nil
}

// splitAptSource splits a one-line "deb [options] uri suite component" entry
// into its options and remaining fields. Only signed-by and arch options are
// accepted.
func splitAptSource(line string) (map[string]string, []string, error) {
	rest, ok := strings.CutPrefix(line, "deb ")
	if !ok {
		return nil, nil, fmt.Errorf("apt-source %q: must start with \"deb \"", line)
	}
	opts := map[string]string{}
	if strings.HasPrefix(rest, "[") {
		inner, after, found := strings.Cut(rest[1:], "]")
		if !found {
			return nil, nil, fmt.Errorf("apt-source %q: unterminated options", line)
		}
		for _, opt := range strings.Fields(inner) {
			key, value, _ := strings.Cut(opt, "=")
			if key != "signed-by" && key != "arch" {
				return nil, nil, fmt.Errorf("apt-source %q: unsupported option %q; only signed-by and arch are "+
					"supported", line, key)
			}
			opts[key] = value
		}
		rest = after
	}
	return opts, strings.Fields(rest), nil
}
