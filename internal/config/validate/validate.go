package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config/schema"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	imageSchemaName     = "os-image-template.schema.json"
	configSchemaName    = "image-composer-tool-config.schema.json"
	chrootenvSchemaName = "chrootenv-config.schema.json"
	osConfigSchemaName  = "os-config.schema.json"
	userRef             = "#/$defs/UserTemplate"
	fullRef             = "#/$defs/FullTemplate"
)

var log = logger.Logger()

// registerImageTemplateFormats adds format checkers referenced by
// os-image-template.schema.json. santhosh-tekuri/jsonschema does not ship
// ipv4-cidr / ipv6-cidr, and draft 2020-12 only asserts formats when
// Compiler.AssertFormat is true.
func registerImageTemplateFormats(c *jsonschema.Compiler) {
	c.Formats["ipv4-cidr"] = func(v interface{}) bool {
		s, ok := v.(string)
		if !ok {
			return true
		}
		ip, _, err := net.ParseCIDR(s)
		if err != nil {
			return false
		}
		return ip.To4() != nil
	}
	c.Formats["ipv6-cidr"] = func(v interface{}) bool {
		s, ok := v.(string)
		if !ok {
			return true
		}
		ip, _, err := net.ParseCIDR(s)
		if err != nil {
			return false
		}
		return ip.To4() == nil
	}
}

// ValidateAgainstSchema compiles the given schema bytes and runs it against
// the JSON in data.  The `name` is only used to identify the schema in errors.
func ValidateAgainstSchema(name string, schemaBytes, data []byte, ref string) error {
	comp := jsonschema.NewCompiler()
	if name == imageSchemaName {
		comp.AssertFormat = true
		registerImageTemplateFormats(comp)
	}
	if err := comp.AddResource(name, bytes.NewReader(schemaBytes)); err != nil {
		log.Errorf("Error loading schema %q: %v", name, err)
		return fmt.Errorf("loading schema %q: %w", name, err)
	}

	// If ref is empty we compile the root; otherwise compile the subschema.
	target := name
	if ref != "" {
		switch {
		case strings.HasPrefix(ref, "#"):
			target = name + ref
		case strings.HasPrefix(ref, "/"):
			target = name + "#" + ref
		default:
			// treat as anchor name (e.g., "UserTemplate")
			target = name + "#" + ref
		}
	}
	sch, err := comp.Compile(target)
	if err != nil {
		log.Errorf("Error compiling schema %q: %v", name, err)
		return fmt.Errorf("compiling schema %q: %w", name, err)
	}

	// unmarshal into interface{} so the validator can walk it
	var doc interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		log.Errorf("Invalid JSON for %q: %v", name, err)
		return fmt.Errorf("invalid JSON for %q: %w", name, err)
	}
	if err := sch.Validate(doc); err != nil {
		log.Errorf("Schema validation against %q failed: %v", name, err)
		return fmt.Errorf("schema validation against %q failed: %w", name, err)
	}
	return nil
}

// ValidateImageTemplateJSON runs the template schema against data
func ValidateImageTemplateJSON(data []byte) error {
	if err := ValidateAgainstSchema(
		imageSchemaName,
		schema.ImageTemplateSchema,
		data,
		fullRef,
	); err != nil {
		return err
	}

	if err := validateAutoExpandLastPartitionConstraints(data, true); err != nil {
		return err
	}

	if err := validateFDEConstraints(data); err != nil {
		return err
	}

	return nil
}

// User-provided (minimal) template
func ValidateUserTemplateJSON(data []byte) error {
	if err := ValidateAgainstSchema(
		imageSchemaName,
		schema.ImageTemplateSchema,
		data,
		userRef,
	); err != nil {
		return err
	}

	if err := validateAutoExpandLastPartitionConstraints(data, false); err != nil {
		return err
	}

	if err := validateFDEConstraints(data); err != nil {
		return err
	}

	return nil
}

// ValidateConfigJSON runs the config schema against data
func ValidateConfigJSON(data []byte) error {
	return ValidateAgainstSchema(
		configSchemaName,
		schema.ConfigSchema,
		data,
		"",
	)
}

func validateAutoExpandLastPartitionConstraints(data []byte, requirePartitions bool) error {
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("invalid JSON for auto-expand validation: %w", err)
	}

	disk, _ := doc["disk"].(map[string]interface{})
	extendEnabled, _ := disk["extendLastPartitionToFillDisk"].(bool)
	if !extendEnabled {
		return nil
	}

	target, _ := doc["target"].(map[string]interface{})
	imageType, _ := target["imageType"].(string)
	if imageType == "iso" {
		return fmt.Errorf("first-boot partition auto-expand does not support imageType=%q", imageType)
	}
	if imageType != "raw" {
		return nil
	}

	systemConfig, _ := doc["systemConfig"].(map[string]interface{})
	immutability, _ := systemConfig["immutability"].(map[string]interface{})
	if enabled, _ := immutability["enabled"].(bool); enabled {
		return fmt.Errorf("first-boot partition auto-expand requires immutability to be disabled")
	}

	partitionsRaw, foundPartitions := disk["partitions"]
	if !foundPartitions {
		if requirePartitions {
			return fmt.Errorf("first-boot partition auto-expand requires at least one disk partition")
		}
		return nil
	}

	partitions, _ := partitionsRaw.([]interface{})
	if len(partitions) == 0 {
		return fmt.Errorf("first-boot partition auto-expand requires at least one disk partition")
	}

	lastPartition, ok := partitions[len(partitions)-1].(map[string]interface{})
	if !ok {
		return fmt.Errorf("first-boot partition auto-expand requires a valid last disk partition definition")
	}

	mountPoint, _ := lastPartition["mountPoint"].(string)
	if mountPoint != "/" {
		return fmt.Errorf("first-boot partition auto-expand requires the last partition to be rootfs ('/'), got mountpoint=%q", mountPoint)
	}

	return nil
}

// validateFDEConstraints ensures a non-empty passphrase when FDE is enabled.
// The same rule exists in os-image-template.schema.json; this check mirrors
// validateAutoExpandLastPartitionConstraints for rules enforced in Go after
// schema validation.
func validateFDEConstraints(data []byte) error {
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("invalid JSON for FDE validation: %w", err)
	}

	systemConfig, _ := doc["systemConfig"].(map[string]interface{})
	if systemConfig == nil {
		return nil
	}

	fde, _ := systemConfig["fde"].(map[string]interface{})
	if fde == nil {
		return nil
	}

	enabled, _ := fde["enabled"].(bool)
	if !enabled {
		return nil
	}

	passphrase, _ := fde["passphrase"].(string)
	if strings.TrimSpace(passphrase) == "" {
		return fmt.Errorf("systemConfig.fde.passphrase is required when fde.enabled is true")
	}

	return nil
}
