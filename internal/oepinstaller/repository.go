package oepinstaller

import (
	"fmt"
	"strings"
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
	return repo, nil
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
