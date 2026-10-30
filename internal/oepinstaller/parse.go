package oepinstaller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/open-edge-platform/image-composer-tool/internal/config/schema"
	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
)

const schemaName = "oep-installer-sbom.schema.json"

// document mirrors the keys of one output document.
type document struct {
	SchemaVersion     string   `yaml:"schema-version"`
	CLIVersion        string   `yaml:"cli-version"`
	CLIURL            string   `yaml:"cli-url"`
	ModuleList        []string `yaml:"module-list"`
	InstallCmdline    []string `yaml:"install-cmdline"`
	PkgList           []string `yaml:"pkg-list"`
	KernelCmdline     []string `yaml:"kernel-cmdline"`
	RepoName          string   `yaml:"repo-name"`
	GPGKey            string   `yaml:"gpg-key"`
	GPGKeyFingerprint string   `yaml:"gpg-key-fingerprint"`
	KeyFile           string   `yaml:"key-file"`
	AptSource         string   `yaml:"apt-source"`
	AptSrcSource      string   `yaml:"apt-src-source"`
	AptListFile       string   `yaml:"apt-list-file"`
	AptPref           []string `yaml:"apt-pref"`
	AptPrefFile       string   `yaml:"apt-pref-file"`
}

// Parse validates `openedge-cli list --sbom` output produced for modules and
// args and returns its contents. Every document must match the embedded
// schema, the first document must be the only header, the header's
// module-list must equal modules, and the arguments of its install-cmdline,
// when present, must equal args.
func Parse(data []byte, modules, args []string) (*Output, error) {
	docs, err := decodeDocuments(data)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 || docs[0].SchemaVersion == "" {
		return nil, fmt.Errorf("openedge-cli output: the first document must be the header")
	}
	out, err := newOutput(docs[0], modules, args)
	if err != nil {
		return nil, err
	}
	for i, doc := range docs[1:] {
		if err := out.add(doc); err != nil {
			return nil, fmt.Errorf("openedge-cli output document %d: %w", i+1, err)
		}
	}
	return out, nil
}

// newOutput checks the header against the requested modules and arguments.
func newOutput(header document, modules, args []string) (*Output, error) {
	if !slices.Equal(header.ModuleList, modules) {
		return nil, fmt.Errorf("openedge-cli output: module-list %v does not match the requested modules %v",
			header.ModuleList, modules)
	}
	installArgs, err := installArguments(header.InstallCmdline, header.ModuleList)
	if err != nil {
		return nil, err
	}
	if installArgs != nil && !slices.Equal(installArgs, args) {
		return nil, fmt.Errorf("openedge-cli output: install-cmdline arguments %v do not match the requested "+
			"arguments %v", installArgs, args)
	}
	return &Output{CLIVersion: header.CLIVersion, CLIURL: header.CLIURL, Modules: header.ModuleList,
		InstallCmdline: header.InstallCmdline, Arguments: installArgs}, nil
}

// installArguments returns the arguments of an install-cmdline of the form
// [<cli>, "install", <modules>..., <arguments>...], or nil when it is absent.
func installArguments(cmdline, modules []string) ([]string, error) {
	if cmdline == nil {
		return nil, nil
	}
	n := len(modules)
	if len(cmdline) < 2+n || cmdline[1] != "install" || !slices.Equal(cmdline[2:2+n], modules) {
		return nil, fmt.Errorf("openedge-cli output: install-cmdline %q must be the CLI, \"install\", "+
			"the module-list %v, then any arguments", cmdline, modules)
	}
	return append([]string{}, cmdline[2+n:]...), nil
}

// decodeDocuments splits the YAML stream, skips empty documents, and
// validates each remaining document against the schema.
func decodeDocuments(data []byte) ([]document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []document
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("openedge-cli output: invalid YAML: %w", err)
		}
		if isEmptyDocument(&node) {
			continue
		}
		doc, err := decodeDocument(&node)
		if err != nil {
			return nil, fmt.Errorf("openedge-cli output document %d: %w", len(docs), err)
		}
		docs = append(docs, doc)
	}
}

func isEmptyDocument(node *yaml.Node) bool {
	return node.Kind == 0 || (node.Kind == yaml.DocumentNode &&
		(len(node.Content) == 0 || node.Content[0].Tag == "!!null"))
}

func decodeDocument(node *yaml.Node) (document, error) {
	var generic interface{}
	if err := node.Decode(&generic); err != nil {
		return document{}, fmt.Errorf("decoding: %w", err)
	}
	asJSON, err := json.Marshal(generic)
	if err != nil {
		return document{}, fmt.Errorf("converting to JSON: %w", err)
	}
	if err := validate.ValidateAgainstSchema(schemaName, schema.OEPInstallerSBOMSchema, asJSON, ""); err != nil {
		return document{}, err
	}
	var doc document
	if err := node.Decode(&doc); err != nil {
		return document{}, fmt.Errorf("decoding: %w", err)
	}
	return doc, nil
}

// add appends one component document to out.
func (out *Output) add(doc document) error {
	if doc.SchemaVersion != "" {
		return fmt.Errorf("only the first document may be a header")
	}
	for _, entry := range doc.PkgList {
		out.Packages = append(out.Packages, classifyPackage(entry))
	}
	out.KernelCmdline = append(out.KernelCmdline, doc.KernelCmdline...)
	if doc.AptSource == "" {
		return nil
	}
	repo, err := newRepository(doc)
	if err != nil {
		return err
	}
	out.Repositories = append(out.Repositories, repo)
	return nil
}

// classifyPackage interprets a schema-valid pkg-list entry.
func classifyPackage(entry string) Package {
	p := Package{Entry: entry, Name: entry}
	switch {
	case strings.HasPrefix(entry, "https://") || strings.HasPrefix(entry, "http://"):
		p.Kind, p.Name = PackageURL, ""
	case strings.HasPrefix(entry, "-"):
		p.Kind, p.Name = PackageRemoval, entry[1:]
	case strings.Contains(entry, "="):
		p.Name, p.Version, _ = strings.Cut(entry, "=")
		p.Kind = PackageExactVersion
		if strings.HasSuffix(p.Version, "*") {
			p.Kind, p.Version = PackageVersionPrefix, strings.TrimSuffix(p.Version, "*")
		}
	case strings.ContainsAny(entry, "*?["):
		p.Kind = PackageGlob
	}
	return p
}
