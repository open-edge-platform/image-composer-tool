// internal/config/global.go
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/slice"
	"gopkg.in/yaml.v3"
)

// GlobalConfig holds essential tool-level configuration parameters
type GlobalConfig struct {
	// Core tool settings
	Workers   int    `yaml:"workers" json:"workers"`       // Number of concurrent download workers (1-100, default: 8)
	ConfigDir string `yaml:"config_dir" json:"config_dir"` // Directory for configuration files (default: ./config)
	CacheDir  string `yaml:"cache_dir" json:"cache_dir"`   // Package cache directory where downloaded RPMs/DEBs are stored (default: ./cache)
	WorkDir   string `yaml:"work_dir" json:"work_dir"`     // Working directory for build operations and image assembly (default: ./workspace)
	TempDir   string `yaml:"temp_dir" json:"temp_dir"`     // Temporary directory for short-lived files like GPG keys and metadata parsing (empty = system default)

	// Logging configuration
	Logging LoggingConfig `yaml:"logging" json:"logging"` // Logging behavior settings

	// AI configuration (optional)
	AI AIConfig `yaml:"ai,omitempty" json:"ai,omitempty"` // AI-powered template generation settings
}

// LoggingConfig controls basic logging behavior
type LoggingConfig struct {
	Level string `yaml:"level" json:"level"`                   // Log verbosity level: debug (most verbose), info (default), warn (warnings only), error (errors only)
	File  string `yaml:"file,omitempty" json:"file,omitempty"` // Optional log file path for teeing output to disk
}

// AIConfig holds AI-powered template generation settings
type AIConfig struct {
	Provider     string          `yaml:"provider,omitempty" json:"provider,omitempty"`           // AI provider: "ollama" (default) or "openai"
	TemplatesDir string          `yaml:"templates_dir,omitempty" json:"templates_dir,omitempty"` // Directory containing template files for RAG
	Ollama       OllamaConfig    `yaml:"ollama,omitempty" json:"ollama,omitempty"`               // Ollama-specific settings
	OpenAI       OpenAIConfig    `yaml:"openai,omitempty" json:"openai,omitempty"`               // OpenAI-specific settings
	Cache        AICacheConfig   `yaml:"cache,omitempty" json:"cache,omitempty"`                 // Embedding cache settings
	Scoring      AIScoringConfig `yaml:"scoring,omitempty" json:"scoring,omitempty"`             // Hybrid scoring weights
}

// OllamaConfig holds Ollama-specific AI settings
type OllamaConfig struct {
	BaseURL        string `yaml:"base_url,omitempty" json:"base_url,omitempty"`               // Ollama server URL (default: http://localhost:11434)
	EmbeddingModel string `yaml:"embedding_model,omitempty" json:"embedding_model,omitempty"` // Model for embeddings (default: nomic-embed-text)
	ChatModel      string `yaml:"chat_model,omitempty" json:"chat_model,omitempty"`           // Model for chat/generation (default: llama3.2)
	Timeout        string `yaml:"timeout,omitempty" json:"timeout,omitempty"`                 // Request timeout (default: 120s)
}

// OpenAIConfig holds OpenAI-specific AI settings
type OpenAIConfig struct {
	EmbeddingModel string `yaml:"embedding_model,omitempty" json:"embedding_model,omitempty"` // Model for embeddings (default: text-embedding-3-small)
	ChatModel      string `yaml:"chat_model,omitempty" json:"chat_model,omitempty"`           // Model for chat/generation (default: gpt-4o-mini)
	Timeout        string `yaml:"timeout,omitempty" json:"timeout,omitempty"`                 // Request timeout (default: 60s)
}

// AICacheConfig holds embedding cache settings
type AICacheConfig struct {
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"` // Enable caching (default: true)
	Dir     string `yaml:"dir,omitempty" json:"dir,omitempty"`         // Cache directory (default: ./cache/ai-embeddings)
}

// AIScoringConfig holds hybrid scoring weights for template retrieval
type AIScoringConfig struct {
	SemanticWeight float64 `yaml:"semantic_weight,omitempty" json:"semantic_weight,omitempty"` // Embedding similarity weight (default: 0.70)
	KeywordWeight  float64 `yaml:"keyword_weight,omitempty" json:"keyword_weight,omitempty"`   // Keyword matching weight (default: 0.20)
	PackageWeight  float64 `yaml:"package_weight,omitempty" json:"package_weight,omitempty"`   // Package matching weight (default: 0.10)
}

// Global singleton variables
var (
	globalInstance *GlobalConfig
	globalMutex    sync.RWMutex
	once           sync.Once
)

// SetGlobal sets the global config instance (call once at startup in main.go)
func SetGlobal(config *GlobalConfig) {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	globalInstance = config
}

// Global returns the global config instance
func Global() *GlobalConfig {
	once.Do(func() {
		globalMutex.Lock()
		defer globalMutex.Unlock()
		if globalInstance == nil {
			globalInstance = DefaultGlobalConfig()
		}
	})

	globalMutex.RLock()
	defer globalMutex.RUnlock()
	return globalInstance
}

// DefaultGlobalConfig returns a GlobalConfig with sensible defaults
func DefaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Workers:   8,
		ConfigDir: "./config",
		CacheDir:  "./cache",
		WorkDir:   "./workspace",
		TempDir:   "./tmp",

		Logging: LoggingConfig{
			Level: "info",
			File:  "image-composer-tool.log",
		},
	}
}

// LoadGlobalConfig loads configuration from the specified path
func LoadGlobalConfig(configPath string) (*GlobalConfig, error) {
	// Start with defaults
	config := DefaultGlobalConfig()

	// If no config file specified or doesn't exist, return defaults
	if configPath == "" {
		return config, nil
	}

	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return config, nil // Return defaults if file doesn't exist
		}
		if errors.Is(err, os.ErrPermission) {
			log.Warnf("Config file %s is not accessible (%v); using defaults", configPath, err)
			return config, nil
		}
		log.Errorf("Error accessing config file %s: %v", configPath, err)
		return nil, fmt.Errorf("accessing config file %s: %w", configPath, err)
	}

	// Load and merge config file values with symlink protection
	data, err := security.SafeReadFile(configPath, security.RejectSymlinks)
	if err != nil {
		log.Errorf("Error reading config file %s: %v", configPath, err)
		return nil, fmt.Errorf("reading config file %s: %w", configPath, err)
	}

	// Determine format by extension
	ext := strings.ToLower(filepath.Ext(configPath))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, config); err != nil {
			log.Errorf("Error parsing YAML config: %v", err)
			return nil, fmt.Errorf("parsing YAML config: %w", err)
		}

		// Convert to JSON for schema validation
		jsonData, err := json.Marshal(config)
		if err != nil {
			log.Errorf("Error converting config to JSON for validation: %v", err)
			return nil, fmt.Errorf("converting config to JSON for validation: %w", err)
		}

		// Validate against schema
		if err := validate.ValidateConfigJSON(jsonData); err != nil {
			log.Errorf("Schema validation failed: %v", err)
			return nil, fmt.Errorf("schema validation failed: %w", err)
		}

	default:
		log.Errorf("Unsupported config file format: %s", ext)
		return nil, fmt.Errorf("unsupported config file format: %s (supported: .yaml, .yml)", ext)
	}

	// Validate the final configuration
	if err := config.Validate(); err != nil {
		log.Errorf("Config validation failed: %v", err)
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

// SaveGlobalConfig saves the configuration to the specified path
func (gc *GlobalConfig) SaveGlobalConfig(configPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			log.Errorf("Failed to create config directory: %v", err)
			return fmt.Errorf("creating config directory: %w", err)
		}
	}

	// Convert to JSON for schema validation before saving
	jsonData, err := json.Marshal(gc)
	if err != nil {
		log.Errorf("Error converting config to JSON for validation: %v", err)
		return fmt.Errorf("converting config to JSON for validation: %w", err)
	}

	if err := validate.ValidateConfigJSON(jsonData); err != nil {
		log.Errorf("Config validation failed before save: %v", err)
		return fmt.Errorf("config validation failed before save: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(gc)
	if err != nil {
		log.Errorf("Error marshaling config to YAML: %v", err)
		return fmt.Errorf("marshaling config to YAML: %w", err)
	}

	// Use safe write to prevent symlink attacks
	if err := security.SafeWriteFile(configPath, data, 0600, security.RejectSymlinks); err != nil {
		log.Errorf("Error writing config file: %v", err)
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// SaveGlobalConfigWithComments saves the configuration with descriptive comments
// mirroring the sample configuration shipped with the project. Primarily used by
// the CLI config init command to create a user-friendly starting file.
func (gc *GlobalConfig) SaveGlobalConfigWithComments(configPath string) error {
	if configPath == "" {
		return fmt.Errorf("config path is empty")
	}

	dir := filepath.Dir(configPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			log.Errorf("Failed to create config directory: %v", err)
			return fmt.Errorf("creating config directory: %w", err)
		}
	}

	jsonData, err := json.Marshal(gc)
	if err != nil {
		log.Errorf("Error converting config to JSON for validation: %v", err)
		return fmt.Errorf("converting config to JSON for validation: %w", err)
	}

	if err := validate.ValidateConfigJSON(jsonData); err != nil {
		log.Errorf("Config validation failed before save: %v", err)
		return fmt.Errorf("config validation failed before save: %w", err)
	}

	commented := gc.renderCommentedYAML()

	if err := security.SafeWriteFile(configPath, []byte(commented), 0600, security.RejectSymlinks); err != nil {
		log.Errorf("Error writing config file: %v", err)
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// renderCommentedYAML builds a YAML representation of the config with rich comments.
func (gc *GlobalConfig) renderCommentedYAML() string {
	var b strings.Builder

	b.WriteString("# ICT - Global Configuration\n")
	b.WriteString("# This file contains tool-level settings that apply across all image builds.\n")
	b.WriteString("# Image-specific parameters should be defined in the image specification.\n\n")

	b.WriteString("# Core tool settings\n")
	fmt.Fprintf(&b, "workers: %d\n", gc.Workers)
	b.WriteString("# Number of concurrent download workers (1-100, default: 8)\n")
	b.WriteString("# Higher values speed up package downloads but consume more network/CPU resources\n")
	b.WriteString("# Recommended: 8-16 for most systems, 20+ for high-bandwidth servers\n\n")

	fmt.Fprintf(&b, "config_dir: %q\n", gc.ConfigDir)
	b.WriteString("# Directory containing configuration files for different target OSs (default: ./config)\n")
	b.WriteString("# Should contain subdirectories for general and each target OS config files.\n\n")

	fmt.Fprintf(&b, "cache_dir: %q\n", gc.CacheDir)
	b.WriteString("# Package cache directory where downloaded RPMs/DEBs are stored (default: ./cache)\n")
	b.WriteString("# This directory persists between builds for package reuse\n")
	b.WriteString("# Should have sufficient space (typically 1-5GB depending on image size)\n\n")

	fmt.Fprintf(&b, "work_dir: %q\n", gc.WorkDir)
	b.WriteString("# Working directory for build operations and image assembly (default: ./workspace)\n")
	b.WriteString("# Contains temporary build artifacts, extracted packages, and final images\n")
	b.WriteString("# Hosts the per-provider chrootenv/chrootbuild trees used for entering/exiting chroot\n")
	b.WriteString("# Requires substantial space during builds (2-10GB typical)\n\n")

	fmt.Fprintf(&b, "temp_dir: %q\n", gc.TempDir)
	b.WriteString("# Temporary directory for short-lived files like GPG keys and metadata parsing\n")
	b.WriteString("# Empty value uses system default (/tmp on Linux, %TEMP% on Windows)\n")
	b.WriteString("# Used for: GPG verification files, decompressed metadata, parsing operations\n")
	b.WriteString("# Files here are deleted within seconds/minutes of creation\n\n")

	b.WriteString("# Logging configuration\n")
	b.WriteString("logging:\n")
	fmt.Fprintf(&b, "  level: %q\n", gc.Logging.Level)
	b.WriteString("  # Log verbosity level (default: info)\n")
	b.WriteString("  # - debug: Most verbose, shows all operations and data structures\n")
	b.WriteString("  # - info:  Normal output, shows progress and important events\n")
	b.WriteString("  # - warn:  Only warnings and errors, minimal output\n")
	b.WriteString("  # - error: Only errors, very quiet operation\n")
	if gc.Logging.File != "" {
		fmt.Fprintf(&b, "  file: %q\n", gc.Logging.File)
		b.WriteString("  # Tee logs to this file in addition to stdout/stderr (overwritten on each run)\n")
	}

	return b.String()
}

// Validate checks the configuration for consistency and applies constraints
// Note: This should NOT set defaults - that's done in DefaultGlobalConfig()
func (gc *GlobalConfig) Validate() error {
	// Validate workers range
	if gc.Workers <= 0 {
		log.Errorf("Workers must be greater than 0, got %d", gc.Workers)
		return fmt.Errorf("workers must be greater than 0, got %d", gc.Workers)
	}
	if gc.Workers > 100 {
		log.Errorf("Workers cannot exceed 100, got %d", gc.Workers)
		return fmt.Errorf("workers cannot exceed 100, got %d", gc.Workers)
	}

	// Validate required fields are not empty
	if gc.CacheDir == "" {
		log.Errorf("CacheDir cannot be empty")
		return fmt.Errorf("CacheDir cannot be empty")
	}
	if gc.WorkDir == "" {
		log.Errorf("WorkDir cannot be empty")
		return fmt.Errorf("WorkDir cannot be empty")
	}

	// Validate logging level
	validLevels := []string{"debug", "info", "warn", "error"}
	if !slice.Contains(validLevels, gc.Logging.Level) {
		return fmt.Errorf("invalid log level %q, must be one of: %s",
			gc.Logging.Level, strings.Join(validLevels, ", "))
	}

	gc.Logging.File = strings.TrimSpace(gc.Logging.File)

	// Ensure temp directory is set (can be empty to use system default)
	if gc.TempDir == "" {
		gc.TempDir = os.TempDir()
	}

	return nil
}

// GetConfigPaths returns the standard configuration file paths to check
func GetConfigPaths() []string {
	homeDir, _ := os.UserHomeDir()

	paths := []string{
		"image-composer-tool.yml",   // Primary config location (root directory)
		".image-composer-tool.yml",  // Hidden file in current directory
		"image-composer-tool.yaml",  // Alternative extension
		".image-composer-tool.yaml", // Hidden file alternative
	}

	if homeDir != "" {
		paths = append(paths,
			filepath.Join(homeDir, ".image-composer-tool", "config.yml"),
			filepath.Join(homeDir, ".image-composer-tool", "config.yaml"),
			filepath.Join(homeDir, ".config", "image-composer-tool", "config.yml"),
			filepath.Join(homeDir, ".config", "image-composer-tool", "config.yaml"),
		)
	}

	// System-wide config paths
	paths = append(paths,
		"/etc/image-composer-tool/config.yml",
		"/etc/image-composer-tool/config.yaml",
	)

	return paths
}

// FindConfigFile searches for a configuration file in standard locations
func FindConfigFile() string {
	for _, path := range GetConfigPaths() {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// Convenience functions that can be used anywhere in the codebase
func Workers() int {
	return Global().Workers
}

func VerificationWorkers() int {
	workers := Global().Workers
	if workers > 4 {
		workers = 4 // Keep the cap as requested
	}
	return workers
}

func ConfigDir() (string, error) {
	configDir, err := filepath.Abs(Global().ConfigDir)
	if err != nil {
		log.Errorf("Failed to resolve config directory: %v", err)
		return "", fmt.Errorf("failed to resolving config directory: %w", err)
	}
	return configDir, nil
}

func CacheDir() (string, error) {
	cacheDir, err := filepath.Abs(Global().CacheDir)
	if err != nil {
		log.Errorf("Failed to resolve cache directory: %v", err)
		return "", fmt.Errorf("failed to resolve cache directory: %w", err)
	}
	return cacheDir, nil
}

func WorkDir() (string, error) {
	workDir, err := filepath.Abs(Global().WorkDir)
	if err != nil {
		log.Errorf("Failed to resolve work directory: %v", err)
		return "", fmt.Errorf("failed to resolve work directory: %w", err)
	}
	return workDir, nil
}

func TempDir() string {
	tempDir := Global().TempDir
	if tempDir == "" {
		tempDir = "./tmp" // Default to ./tmp instead of system temp
	}

	// Ensure directory exists
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		// If we can't create it, return the path anyway
		return tempDir
	}

	return tempDir
}

func LogLevel() string {
	return Global().Logging.Level
}

func IsDebugMode() bool {
	return Global().Logging.Level == "debug"
}

// Directory creation helpers
func EnsureCacheDir() error {
	cacheDir, err := CacheDir()
	if err != nil {
		return fmt.Errorf("resolving cache directory: %w", err)
	}
	return ensureDirExists(cacheDir)
}

func EnsureWorkDir() error {
	workDir, err := WorkDir()
	if err != nil {
		return fmt.Errorf("resolving work directory: %w", err)
	}
	return ensureDirExists(workDir)
}

func EnsureTempDir(subdir string) (string, error) {
	tempDir := filepath.Join(TempDir(), subdir)
	err := ensureDirExists(tempDir)
	return tempDir, err
}

func ensureDirExists(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0700)
	}
	return nil
}

func GetGeneralConfigDir() (string, error) {
	configPath, err := ConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config path: %w", err)
	}
	generalConfigDir := filepath.Join(configPath, "general")
	if _, err := os.Stat(generalConfigDir); os.IsNotExist(err) {
		log.Errorf("General config directory does not exist: %s", generalConfigDir)
		return "", fmt.Errorf("general config directory does not exist: %s", generalConfigDir)
	}
	return generalConfigDir, nil
}

func GetTargetOsConfigDir(targetOs, targetDist string) (string, error) {
	configPath, err := ConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config path: %w", err)
	}
	targetOsConfigPath := filepath.Join(configPath, "osv", targetOs, targetDist)
	if _, err := os.Stat(targetOsConfigPath); os.IsNotExist(err) {
		log.Errorf("Target OS config directory does not exist: %s", targetOsConfigPath)
		return "", fmt.Errorf("target OS config directory does not exist: %s", targetOsConfigPath)
	}
	return targetOsConfigPath, nil
}
