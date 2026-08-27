package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"regexp"
	"strconv"
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

	if _, err := validateDiskMaxSizeConstraints(data); err != nil {
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

	if _, err := validateDiskMaxSizeConstraints(data); err != nil {
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

// validateFDEConstraints ensures a non-empty passphrase file when FDE is enabled.
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

	passphraseFile, _ := fde["passphraseFile"].(string)
	if strings.TrimSpace(passphraseFile) == "" {
		return fmt.Errorf("systemConfig.fde.passphraseFile is required when fde.enabled is true")
	}

	return nil
}

// diskSizeSuffixes/diskSizeSuffixBytes/diskSizePattern mirror config.go's
// parseDiskSizeBytes (itself mirroring imagedisc.TranslateSizeStrToBytes's unit
// table). Duplicated here because this package validates raw JSON before it is
// unmarshaled into config.ImageTemplate, and internal/config already imports
// this package, so it cannot be imported back.
var (
	diskSizeSuffixes    = []string{"KiB", "MiB", "GiB", "K", "M", "G", "KB", "MB", "GB"}
	diskSizeSuffixBytes = []uint64{1024, 1048576, 1073741824, 1024, 1048576, 1073741824, 1000, 1000000, 1000000000}
	diskSizePattern     = regexp.MustCompile(`^(\d+)(.*)$`)
)

// parseDiskSizeBytes parses a disk.size/disk.maxSize string (e.g. "8GiB") into
// bytes, capped at math.MaxInt64 to match the int64 the build-time resize path
// (resolveSizeBytes) narrows to.
func parseDiskSizeBytes(field, s string) (uint64, error) {
	match := diskSizePattern.FindStringSubmatch(s)
	if len(match) != 3 {
		return 0, fmt.Errorf("%s %q: size format incorrect", field, s)
	}
	num, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", field, s, err)
	}
	for i, suf := range diskSizeSuffixes {
		if match[2] == suf {
			unit := diskSizeSuffixBytes[i]
			if num > math.MaxInt64/unit {
				return 0, fmt.Errorf("%s %q: size overflows the supported range", field, s)
			}
			return num * unit, nil
		}
	}
	return 0, fmt.Errorf("%s %q: size suffix %q not recognized", field, s, match[2])
}

// validateDiskMaxSizeConstraints enforces disk.maxSize's invariants against raw
// template JSON: it is only supported when baseline.mode is "overlay", requires
// disk.size to also be set, and must parse to a byte value strictly greater than
// disk.size. This mirrors config.go's validateDiskMaxSize/validateBaseline
// checks so the REST validation path (ValidateUserTemplateIssues) reports the
// same verdict a build would reach, instead of deferring to the mounted resize
// stage. disk.size's format is validated independently of disk.maxSize being
// set, so a malformed disk.size (with no maxSize) is also caught here instead
// of only failing later in ResolveSizeBytes/ResizeBaseline.
//
// Like validateBaseline, the baseline.mode check is skipped when extends is
// set: a layer with extends may be inheriting baseline.mode: overlay from its
// parent without redeclaring it, so this raw, single-layer document is not yet
// authoritative about mode. LoadAndMergeTemplate re-validates the folded result.
//
// It returns the offending field's path ("disk.size" or "disk.maxSize")
// alongside the error, so structured callers (ValidateUserTemplateIssues) can
// anchor the Issue at the field that is actually wrong instead of always
// blaming disk.maxSize. The path is "" when err is nil.
func validateDiskMaxSizeConstraints(data []byte) (string, error) {
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("invalid JSON for disk.maxSize validation: %w", err)
	}

	disk, _ := doc["disk"].(map[string]interface{})
	size := strings.TrimSpace(stringField(disk, "size"))
	var sizeBytes uint64
	if size != "" {
		var err error
		sizeBytes, err = parseDiskSizeBytes("disk.size", size)
		if err != nil {
			return "disk.size", err
		}
	}

	maxSize := strings.TrimSpace(stringField(disk, "maxSize"))
	if maxSize == "" {
		return "", nil
	}

	baseline, _ := doc["baseline"].(map[string]interface{})
	mode := stringField(baseline, "mode")
	extends := strings.TrimSpace(stringField(doc, "extends"))
	if mode != "overlay" && extends == "" {
		return "disk.maxSize", fmt.Errorf("disk.maxSize is only supported when baseline.mode is \"overlay\"")
	}

	if size == "" {
		return "disk.maxSize", fmt.Errorf("disk.maxSize requires disk.size to also be set")
	}

	maxSizeBytes, err := parseDiskSizeBytes("disk.maxSize", maxSize)
	if err != nil {
		return "disk.maxSize", err
	}
	if maxSizeBytes <= sizeBytes {
		return "disk.maxSize", fmt.Errorf("disk.maxSize (%d bytes) must be greater than disk.size (%d bytes)", maxSizeBytes, sizeBytes)
	}
	return "", nil
}

// stringField reads a string field from a possibly-nil JSON object map,
// returning "" when the map is nil or the field is absent/non-string.
func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}
