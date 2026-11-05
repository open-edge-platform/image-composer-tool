package schema

import _ "embed"

//go:embed os-image-template.schema.json
var ImageTemplateSchema []byte

//go:embed image-composer-tool-config.schema.json
var ConfigSchema []byte

// ChrootenvSchema contains the JSON schema for validating chrootenv configuration files
//
//go:embed chrootenv-config.schema.json
var ChrootenvSchema []byte

// OsConfigSchema contains the JSON schema for validating OS configuration files
//
//go:embed os-config.schema.json
var OsConfigSchema []byte

// OEPInstallerSBOMSchema validates each document of `openedge-cli list --sbom`
// output. It must stay identical to the OEP Installer's own validator schema.
//
//go:embed oep-installer-sbom.schema.json
var OEPInstallerSBOMSchema []byte
