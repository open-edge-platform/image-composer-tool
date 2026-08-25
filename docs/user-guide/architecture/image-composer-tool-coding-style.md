# Go Coding Style Guide for ICT

## Table of Contents

1. [General Principles](#1-general-principles)
2. [Code Formatting](#2-code-formatting)
3. [Naming Conventions](#3-naming-conventions)
4. [Error Handling](#4-error-handling)
5. [Logging Guidelines](#5-logging-guidelines)
6. [Modularization](#6-modularization)
7. [Package Design](#7-package-design)
8. [Function Design](#8-function-design)
9. [Testing Guidelines](#9-testing-guidelines)
10. [Performance Best Practices](#10-performance-best-practices)
11. [Security Considerations](#11-security-considerations)
12. [Project-Specific Guidelines](#12-project-specific-guidelines)

## 1. General Principles

### 1.1 Follow Go Idioms

- Write idiomatic Go code that follows community standards
- Use `gofmt` to format all code consistently
- Run `go vet` to catch common mistakes
- Use `golint` for style suggestions

### 1.2 Code Clarity Over Cleverness

```go
// Good: Clear and readable
func isValidImageFormat(format string) bool {
    validFormats := []string{"iso", "qcow2", "raw", "vmdk"}
    for _, valid := range validFormats {
        if format == valid {
            return true
        }
    }
    return false
}

// Avoid: Too clever, hard to understand
func isValidImageFormat(format string) bool {
    return map[string]bool{"iso": true, "qcow2": true, "raw": true, "vmdk": true}[format]
}
```

### 1.3 Consistency

- Maintain consistent patterns across the codebase
- Use the same error handling patterns
- Follow established naming conventions

## 2. Code Formatting

### 2.1 Use gofmt

All code must be formatted with `gofmt`. Set up your editor to run it
automatically on save.

### 2.2 Line Length

- Keep lines under 120 characters when possible
- Break long function signatures and calls appropriately

```go
// Good: Proper line breaking
func BuildImage(
    targetOS string,
    targetDist string,
    targetArch string,
    outputPath string,
    options *BuildOptions,
) error {
    // implementation
}

// Good: Method chaining
result, err := builder.
    SetTargetOS(targetOS).
    SetArchitecture(targetArch).
    SetOutputPath(outputPath).
    Build()
```

### 2.3 Imports

- Group imports in three sections: standard library, third-party, local
- Use blank lines between groups

```go
import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/spf13/cobra"
    "gopkg.in/yaml.v3"

    "github.com/open-edge-platform/image-composer-tool/internal/config"
    "github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)
```

## 3. Naming Conventions

### 3.1 Variables and Functions

- Use camelCase for variables and functions
- Use descriptive names that explain purpose

```go
// Good
var chrootBuildDir string
var packageCacheDir string
func buildChrootEnvironment() error

// Avoid
var dir string
var cache string
func build() error
```

### 3.2 Constants

- Use PascalCase for exported constants and camelCase for unexported constants
- Group related constants

```go
const (
    DefaultImageSize     = "10GB"
    MaxRetryAttempts    = 3
    configFileExtension  = ".yml" // unexported constant
)
```

### 3.3 Types and Interfaces

- Use PascalCase for exported types
- Interface names should end with 'er' when possible

```go
// Good
type ImageBuilder struct {}
type PackageInstaller interface {}
type ConfigReader interface {}

// Specific to ICT
type ChrootBuilder struct {}
type RpmInstaller interface {}
type DebInstaller interface {}
```

### 3.4 Package Names

- Use lowercase, single word package names
- Package names should be descriptive but concise

```go
// Good package structure for ICT
package chroot    // for chroot environment building
package rpm       // for RPM package handling
package deb       // for DEB package handling
package config    // for configuration management
package logger    // for logging utilities
```

## 4. Error Handling

### 4.1 Error Wrapping

Always wrap errors with context using `fmt.Errorf` with `%w` verb:

```go
// Good: Provides context and preserves original error
func downloadPackage(url, destination string) error {
    resp, err := http.Get(url)
    if err != nil {
        return fmt.Errorf("failed to download package from %s: %w", url, err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("download failed with status %d for URL %s", resp.StatusCode, url)
    }

    // ... rest of implementation
    return nil
}
```

### 4.2 Named Return Parameters

Use named returns for complex functions with multiple exit points:

```go
// Good: Clear what's being returned
func createEfiFatImage(isoEfiPath, isoImagesPath string) (efiFatImgPath string, err error) {
    efiFatImgPath = filepath.Join(isoImagesPath, "efiboot.img")

    if err = imagedisc.CreateRawFile(efiFatImgPath, "18MiB"); err != nil {
        err = fmt.Errorf("failed to create EFI FAT image: %w", err)
        return // Bare return uses named parameters
    }

    // ... more implementation
    return // Success case
}
```

### 4.3 Resource Cleanup Patterns

Use defer with named returns instead of "goto fail":

```go
// Good: Use defer for cleanup
func processImageWithCleanup(imagePath string) (err error) {
    tempDir, err := os.MkdirTemp("", "image-process")
    if err != nil {
        return fmt.Errorf("failed to create temp directory: %w", err)
    }

    // Setup cleanup with proper error handling
    defer func() {
        if err == nil {
            // Success cleanup - fail if cleanup fails
            if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
                log.Errorf("Failed to cleanup temp directory: %v", cleanupErr)
                // Update the return error
                err = fmt.Errorf("cleanup failed: %w", cleanupErr)
            }
        } else {
            // Error cleanup - don't override original error
            if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
                log.Errorf("Failed to cleanup temp directory: %v", cleanupErr)
                // Update the return error with warpped original error and cleanup error
                err = fmt.Errorf("operation failed: %w, cleanup errors: %v", err, cleanupErr)
            }
        }
    }()

    // Main processing logic
    if err = processImage(imagePath, tempDir); err != nil {
        return fmt.Errorf("image processing failed: %w", err)
    }

    return nil
}
```

### 4.4 Error Types

Define custom error types for specific error conditions:

```go
// Define custom errors
type ValidationError struct {
    Field   string
    Value   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation failed for field '%s' with value '%s': %s",
        e.Field, e.Value, e.Message)
}

// Usage
func validateImageConfig(config *ImageConfig) error {
    if config.Size <= 0 {
        return &ValidationError{
            Field:   "size",
            Value:   fmt.Sprintf("%d", config.Size),
            Message: "size must be positive",
        }
    }
    return nil
}
```

### 4.5 Error Handling Patterns

```go
// Pattern 1: Early return on error
func buildImage() error {
    if err := validateConfig(); err != nil {
        return fmt.Errorf("config validation failed: %w", err)
    }

    if err := setupEnvironment(); err != nil {
        return fmt.Errorf("environment setup failed: %w", err)
    }

    return nil
}

// Pattern 2: Cleanup on error
func processWithCleanup() (err error) {
    tempDir, err := os.MkdirTemp("", "image-builder")
    if err != nil {
        return fmt.Errorf("failed to create temp directory: %w", err)
    }
    defer func() {
        if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil && err == nil {
            err = fmt.Errorf("cleanup failed: %w", cleanupErr)
        }
    }()

    // ... process implementation
    return nil
}
```

### 4.6 Error and Logging Strategy by Component Type

#### Rule 1: Internal Utilities (`internal/utils`)

Internal utilities should primarily return errors, not log them:

```go
// internal/utils/file/operations.go
package file

// Good: Utility function returns error, doesn't log
func CopyFile(src, dst string) error {
    srcFile, err := os.Open(src)
    if err != nil {
        return fmt.Errorf("failed to open source file %s: %w", src, err)
    }
    defer srcFile.Close()

    // Only use debug logging for utilities when absolutely necessary
    log.Debugf("Copying file from %s to %s", src, dst)

    dstFile, err := os.Create(dst)
    if err != nil {
        return fmt.Errorf("failed to create destination file %s: %w", dst, err)
    }
    defer dstFile.Close()

    if _, err := io.Copy(dstFile, srcFile); err != nil {
        return fmt.Errorf("failed to copy file content: %w", err)
    }

    return nil
}
```

#### Rule 2: Business Logic Functions

Functions that call libraries should both log and return errors:

```go
// internal/chroot/chrootbuild.go
func (cb *ChrootBuilder) installPackages(packages []string) error {
    log.Infof("Installing %d packages in chroot environment", len(packages))

    for _, pkg := range packages {
        if err := cb.packageManager.InstallPackage(pkg); err != nil {
            // Log immediately with business context
            log.Errorf("Failed to install package %s in chroot %s: %v", pkg, cb.chrootPath, err)
            // Return error with context for caller
            return fmt.Errorf("failed to install package %s: %w", pkg, err)
        }
        log.Debugf("Successfully installed package: %s", pkg)
    }

    return nil
}
```

#### Rule 3: High-Level Orchestration Functions

High-level functions should only return errors to avoid duplicate logging:

```go
// cmd/build.go or high-level orchestrators
func buildImageWorkflow(config *BuildConfig) error {
    // Only return errors - logging happens at lower levels
    if err := validateConfig(config); err != nil {
        return fmt.Errorf("configuration validation failed: %w", err)
    }

    if err := setupEnvironment(config); err != nil {
        return fmt.Errorf("environment setup failed: %w", err)
    }

    if err := buildImage(config); err != nil {
        return fmt.Errorf("image build failed: %w", err)
    }

    return nil
}
```

## 5. Logging Guidelines

### 5.1 Global Logger Declaration

Use package-level logger instead of declaring in each function:

```go
// Good: Declare at package level
package chroot

import (
    "github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

var log = logger.Logger()

func (cb *ChrootBuilder) BuildChrootEnv() error {
    log.Infof("Starting chroot environment build")
    // ... implementation
}

// Avoid: Declaring in each function
func (cb *ChrootBuilder) BuildChrootEnv() error {
    log := logger.Logger() // Remove this pattern
    log.Infof("Starting chroot environment build")
    // ... implementation
}
```

### 5.2 Logging Levels

```go
// Info: Major workflow steps
log.Infof("Starting chroot environment build for %s/%s", targetOS, targetArch)

// Error: Business logic errors with context
log.Errorf("Failed to install package %s in chroot %s: %v", packageName, chrootPath, err)

// Debug: Detailed operation info (utilities can use this sparingly)
log.Debugf("Mounting %s at %s with options: %s", device, mountpoint, options)

// Warn: Recoverable issues
log.Warnf("Package %s already installed, skipping", packageName)
```

### 5.3 Error Context in Messages

Add business context to error logs, technical context to error returns:

```go
func (cb *ChrootBuilder) downloadPackage(url, destination string) error {
    if err := downloader.Download(url, destination); err != nil {
        // Log with business context
        log.Errorf("Failed to download package for chroot build, URL: %s, destination: %s: %v",
                   url, destination, err)
        // Return with technical context
        return fmt.Errorf("failed to download package from %s: %w", url, err)
    }
    return nil
}
```

## 6. Modularization

### 6.1 Package Structure

Organize code into logical packages based on functionality:

```text
internal/
├── chroot/           # Chroot environment building
│   ├── chrootbuild/
│   ├── rpm/
│   ├── deb/
│   └── chrootenv.go
├── config/          # Configuration management
│   ├── manifest/
│   ├── schema/
│   ├── testdata/
│   ├── validate/
│   ├── version/
│   ├── config.go
│   ├── global.go
│   └── merge.go
├── image/           # Image building logic
│   ├── imageboot/
│   ├── imageconvert/
│   ├── imagedisc/
│   ├── imageos/
│   ├── imagesecure/
│   ├── imagesign/
│   ├── initrdmaker/
│   ├── isomaker/
│   └── rawmaker/
├── ospackage/       # OS package management
│   ├── debutils/
│   ├── pkgfetcher/
│   ├── pkgsorter/
│   ├── rpmutils/
│   └── ospackage.go
├── provider/        # OSV provider management
│   ├── azl/
│   ├── elxr/
│   ├── emt/
│   └── provider.go
└── utils/           # Shared utilities
    ├── compression/ # Compression utilities
    ├── file/        # File operations
    ├── logger/      # Logging
    ├── mount/       # mount operations
    ├── shell/       # Shell command execution
    ├── slice/       # data structure slice operations
    └── system/      # system command execution
```

### 6.2 Struct-Based Component Design with Interface-Based Dependency Injection

Prefer struct-based approach over global variables:

#### Global State vs Struct-Based Comparison

| Aspect | Global State | Struct-based |
|--------|--------------|-------------|
| Thread Safety | ❌ Race conditions | ✅ Instance isolation |
| Testability | ❌ Shared state pollution | ✅ Clean, isolated tests |
| Multiple Instances | ❌ Only one configuration | ✅ Multiple concurrent instances |
| Ownership | ❌ Unclear lifecycle | ✅ Clear ownership |
| Encapsulation | ❌ Public access | ✅ Controlled access |
| Debugging | ❌ Hard to trace changes | ✅ Built-in debugging support |
| Mocking | ❌ Difficult to mock | ✅ Easy dependency injection |

#### Struct-Based Component Implementation Guidelines

```go
// Good: Struct-based with dependency injection
type ChrootBuilder struct {
    config        *Config
    chrootPath    string
    cacheDir      string
    packageMgr    PackageManager
    fileSystem    FileSystemInterface
    mountManager  MountManager
}

func NewChrootBuilder(
    config *Config,
    packageMgr PackageManager,
    fs FileSystemInterface,
    mountMgr MountManager,
) *ChrootBuilder {
    return &ChrootBuilder{
        config:       config,
        chrootPath:   filepath.Join(config.WorkDir, "chroot"),
        cacheDir:     config.CacheDir,
        packageMgr:   packageMgr,
        fileSystem:   fs,
        mountManager: mountMgr,
    }
}

// Avoid: Global variables
var (
    globalChrootPath string
    globalCacheDir   string
    globalPackageMgr PackageManager
)
```

### 6.3 Function Wrapping Guidelines

#### Rules for Wrapping Functions in Structs

**Should be wrapped (methods):**

- Functions that maintain state between calls
- Functions that are part of the component workflow
- Functions that use struct state/fields
- Functions that need to be mocked for testing

**Should NOT be wrapped (standalone functions):**

- Functions that are stateless utilities
- Functions that don't use struct state
- Functions that could be used independently
- Pure computational functions

```go
// Should be methods (use struct state)
func (cb *ChrootBuilder) buildChrootEnv() error {
    // Uses cb.chrootPath, cb.config, etc.
}

func (cb *ChrootBuilder) installPackages(packages []string) error {
    // Uses cb.packageMgr, cb.chrootPath
}

// Should be standalone functions (stateless utilities)
func validateTargetOS(targetOS string) error {
    supportedOS := []string{"ubuntu", "centos", "fedora"}
    // ... validation logic
}

func parseVersion(versionStr string) (*Version, error) {
    // Pure parsing function
}
```

### 6.4 Interface-Based Dependency Injection Design

Design interfaces that are focused and composable:

#### Rule 1: Define Comprehensive Interfaces

Create interfaces that define all operations a component needs, enabling
complete mockability.

```go
// Good: Comprehensive interface defining all chroot operations
type ChrootEnvInterface interface {
    // Configuration access
    GetChrootEnvRoot() string
    GetTargetOsPkgType() string
    GetTargetOsConfigDir() string

    // Path operations
    GetChrootEnvHostPath(chrootPath string) (string, error)
    GetChrootEnvPath(ChrootEnvHostPath string) (string, error)

    // Mount operations
    MountChrootSysfs(chrootPath string) error
    UmountChrootSysfs(chrootPath string) error
    MountChrootPath(hostFullPath, chrootPath, mountFlags string) error
    UmountChrootPath(chrootPath string) error

    // File operations
    CopyFileFromHostToChroot(hostFilePath, chrootPath string) error
    CopyFileFromChrootToHost(hostFilePath, chrootPath string) error

    // Environment lifecycle
    InitChrootEnv(targetOs, targetDist, targetArch string) error
    CleanupChrootEnv(targetOs, targetDist, targetArch string) error

    // Package management
    TdnfInstallPackage(packageName, installRoot string, repositoryIDList []string) error
    AptInstallPackage(packageName, installRoot string, repoSrcList []string) error
}
```

#### Rule 2: Constructor-Based Dependency Injection

Inject dependencies through constructors, not as method parameters:

```go
// Good: Dependencies injected at construction time
type ChrootEnv struct {
    ChrootEnvRoot       string
    ChrootImageBuildDir string
    ChrootBuilder       chrootbuild.ChrootBuilderInterface  // Injected dependency
}

func NewChrootEnv(targetOs, targetDist, targetArch string) (*ChrootEnv, error) {
    // Create dependency
    chrootBuilder, err := chrootbuild.NewChrootBuilder(targetOs, targetDist, targetArch)
    if err != nil {
        return nil, fmt.Errorf("failed to create chroot builder: %w", err)
    }

    return &ChrootEnv{
        ChrootEnvRoot: chrootEnvRoot,
        ChrootBuilder: chrootBuilder,  // Inject dependency
    }, nil
}

// Avoid: Passing dependencies as method parameters repeatedly
func (ce *ChrootEnv) SomeOperation(builder chrootbuild.ChrootBuilderInterface) error {
    // Don't do this - inject at construction time instead
}
```

#### Rule 3: Delegate to Injected Dependencies for Sub Components

If the sub component's function will be used outside,
use the struct to orchestrate calls to injected dependencies:

```go
// Good: Orchestration through delegation
func (chrootEnv *ChrootEnv) GetTargetOsPkgType() string {
    return chrootEnv.ChrootBuilder.GetTargetOsPkgType()
}

func (chrootEnv *ChrootEnv) GetChrootEnvEssentialPackageList() ([]string, error) {
    return chrootEnv.ChrootBuilder.GetChrootEnvEssentialPackageList()
}

func (chrootEnv *ChrootEnv) GetTargetOsReleaseVersion() string {
    targetOsConfig := chrootEnv.ChrootBuilder.GetTargetOsConfig()
    releaseVersion, ok := targetOsConfig["releaseVersion"]
    if !ok {
        log.Errorf("releaseVersion not found in target OS config")
        return "unknown"
    }
    return releaseVersion.(string)
}
```

#### Rule 4: Interface Composition for Complex Dependencies

When a component needs multiple interfaces, compose them rather than creating
monolithic interfaces

#### Rule 5: Factory Pattern for Complex Construction

Use factory functions when dependency creation is complex

## 7. Package Design

### 7.1 Package Responsibility

Each package should have a single, well-defined responsibility:

```go
// package config - handles all configuration operations
package config

type Reader interface {
    ReadConfig(path string) (*Config, error)
}

type Validator interface {
    ValidateConfig(config *Config) error
}

type Writer interface {
    WriteConfig(config *Config, path string) error
}
```

### 7.2 Public API Design

Keep public APIs minimal and stable:

```go
// Good: Clean public API
package chroot

// Public interface
type Builder interface {
    BuildChrootEnv(targetOS, targetDist, targetArch string) error
    GetChrootPath() string
    Cleanup() error
}

// Public constructor
func NewBuilder(configDir string, options ...BuilderOption) (Builder, error) {
    // implementation
}

// Internal implementation
type chrootBuilder struct {
    // private fields
}

func (cb *chrootBuilder) BuildChrootEnv(targetOS, targetDist, targetArch string) error {
    // implementation
}
```

### 7.3 Configuration Patterns

Use functional options for flexible configuration:

```go
type BuilderOption func(*chrootBuilder) error

func WithCacheDir(dir string) BuilderOption {
    return func(cb *chrootBuilder) error {
        if dir == "" {
            return errors.New("cache directory cannot be empty")
        }
        cb.cacheDir = dir
        return nil
    }
}

func WithTimeout(timeout time.Duration) BuilderOption {
    return func(cb *chrootBuilder) error {
        cb.timeout = timeout
        return nil
    }
}

// Usage
builder, err := chroot.NewBuilder(
    configDir,
    chroot.WithCacheDir("/tmp/cache"),
    chroot.WithTimeout(30*time.Minute),
)
```

## 8. Function Design

### 8.1 Function Length

Keep functions short and focused (typically under 50 lines):

```go
// Good: Single responsibility, easy to test
func validateTargetOS(targetOS string) error {
    supportedOS := []string{"ubuntu", "centos", "fedora", "debian"}
    for _, os := range supportedOS {
        if targetOS == os {
            return nil
        }
    }
    return fmt.Errorf("unsupported target OS: %s", targetOS)
}

func validateTargetArch(targetArch string) error {
    supportedArch := []string{"amd64", "arm64", "armhf"}
    for _, arch := range supportedArch {
        if targetArch == arch {
            return nil
        }
    }
    return fmt.Errorf("unsupported target architecture: %s", targetArch)
}
```

### 8.2 Parameter Lists

Limit the number of parameters (max 4-5), use structs for complex parameter sets:

```go
// Avoid: Too many parameters
func buildImage(targetOS, targetDist, targetArch, outputPath, tempDir, cacheDir string, timeout int, verbose bool) error

// Good: Use a config struct
type BuildConfig struct {
    TargetOS    string
    TargetDist  string
    TargetArch  string
    OutputPath  string
    TempDir     string
    CacheDir    string
    Timeout     time.Duration
    Verbose     bool
}

func buildImage(config *BuildConfig) error {
    // implementation
}
```

### 8.3 Return Values

Be consistent with return patterns:

```go
// Good: Consistent error handling
func createDirectory(path string) error {
    if err := os.MkdirAll(path, 0700); err != nil {
        return fmt.Errorf("failed to create directory %s: %w", path, err)
    }
    return nil
}

// Good: Multiple returns with clear meaning
func findPackage(name string) (path string, found bool, err error) {
    // implementation
}
```

## 9. Testing Guidelines

### 9.1 Test Structure

Follow the AAA pattern (Arrange, Act, Assert):

```go
func TestChrootBuilder_BuildChrootEnv(t *testing.T) {
    tests := []struct {
        name          string
        config        *BuildConfig
        setupFunc     func(*testing.T) string // returns temp dir
        expectedError string
    }{
        {
            name: "successful_build",
            config: &BuildConfig{
                TargetOS:   "ubuntu",
                TargetDist: "20.04",
                TargetArch: "amd64",
            },
            setupFunc:     setupValidEnvironment,
            expectedError: "",
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Arrange
            tempDir := tt.setupFunc(t)
            defer os.RemoveAll(tempDir)

            builder := NewChrootBuilder(tempDir)

            // Act
            err := builder.BuildChrootEnv(tt.config.TargetOS, tt.config.TargetDist, tt.config.TargetArch)

            // Assert
            if tt.expectedError == "" {
                assert.NoError(t, err)
            } else {
                assert.Error(t, err)
                assert.Contains(t, err.Error(), tt.expectedError)
            }
        })
    }
}
```

### 9.2 Mocking

Use interfaces for dependencies to enable easy mocking:

```go
// Mock implementation
type mockPackageInstaller struct {
    shouldFail bool
    installedPackages []string
}

func (m *mockPackageInstaller) InstallPackage(packagePath string) error {
    if m.shouldFail {
        return errors.New("mock installation failure")
    }
    m.installedPackages = append(m.installedPackages, packagePath)
    return nil
}
```

### 9.3 Test Utilities

Create test utilities for common setup:

```go
// testutils/setup.go
func CreateTempDir(t *testing.T) string {
    dir, err := os.MkdirTemp("", "test")
    require.NoError(t, err)
    t.Cleanup(func() {
        os.RemoveAll(dir)
    })
    return dir
}

func CreateMockConfig(targetOS, targetDist, targetArch string) *BuildConfig {
    return &BuildConfig{
        TargetOS:   targetOS,
        TargetDist: targetDist,
        TargetArch: targetArch,
        Timeout:    30 * time.Second,
    }
}
```

## 10. Performance Best Practices

### 10.1 Memory Management

```go
// Good: Reuse slices when possible
func processPackages(packages []string) error {
    results := make([]ProcessResult, 0, len(packages)) // Pre-allocate capacity

    for _, pkg := range packages {
        result, err := processPackage(pkg)
        if err != nil {
            return fmt.Errorf("failed to process package %s: %w", pkg, err)
        }
        results = append(results, result)
    }
    return nil
}

// Good: Use string builder for concatenation
func buildPackageList(packages []string) string {
    var builder strings.Builder
    builder.Grow(len(packages) * 20) // Estimate capacity

    for i, pkg := range packages {
        if i > 0 {
            builder.WriteString(", ")
        }
        builder.WriteString(pkg)
    }
    return builder.String()
}
```

### 10.2 Concurrency

```go
// Good: Use worker pools for concurrent processing
func downloadPackagesConcurrently(urls []string, workers int) error {
    urlChan := make(chan string, len(urls))
    errChan := make(chan error, len(urls))

    // Start workers
    var wg sync.WaitGroup
    for i := 0; i < workers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for url := range urlChan {
                if err := downloadPackage(url); err != nil {
                    errChan <- fmt.Errorf("failed to download %s: %w", url, err)
                    return
                }
            }
        }()
    }

    // Send work
    for _, url := range urls {
        urlChan <- url
    }
    close(urlChan)

    // Wait for completion
    go func() {
        wg.Wait()
        close(errChan)
    }()

    // Check for errors
    for err := range errChan {
        if err != nil {
            return err
        }
    }

    return nil
}
```

## 11. Security Considerations

### 11.1 Input Validation

Always validate inputs, especially from external sources:

```go
func validateImageConfig(config *ImageConfig) error {
    if config.TargetOS == "" {
        return errors.New("target OS cannot be empty")
    }

    // Validate path traversal
    if strings.Contains(config.OutputPath, "..") {
        return errors.New("output path cannot contain path traversal")
    }

    // Validate allowed values
    allowedFormats := map[string]bool{"iso": true, "qcow2": true, "raw": true}
    if !allowedFormats[config.Format] {
        return fmt.Errorf("unsupported image format: %s", config.Format)
    }

    return nil
}
```

### 11.2 Command Execution

Be careful when executing shell commands:

```go
// Good: Use proper escaping and validation
func createFileSystem(devicePath, fsType string) error {
    // Validate inputs
    if !isValidDevicePath(devicePath) {
        return fmt.Errorf("invalid device path: %s", devicePath)
    }

    allowedFS := map[string]bool{"ext4": true, "xfs": true, "vfat": true}
    if !allowedFS[fsType] {
        return fmt.Errorf("unsupported filesystem type: %s", fsType)
    }

    // Use exec.Command instead of shell
    cmd := exec.Command("mkfs", "-t", fsType, devicePath)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to create filesystem: %w", err)
    }

    return nil
}
```

### 11.3 File Permissions

Set appropriate file permissions:

```go
const (
    FilePermission     = 0644
    DirPermission      = 0755
    ExecutablePermission = 0755
    SecretPermission   = 0600
)

func writeConfigFile(path string, data []byte) error {
    return os.WriteFile(path, data, FilePermission)
}

func writeSecretFile(path string, data []byte) error {
    return os.WriteFile(path, data, SecretPermission)
}
```

## 12. Project-Specific Guidelines

### 12.1 Image Builder Patterns

```go
// Use builder pattern for complex image construction
type ImageBuilder struct {
    config *ImageConfig
    steps  []BuildStep
}

func (ib *ImageBuilder) AddStep(step BuildStep) *ImageBuilder {
    ib.steps = append(ib.steps, step)
    return ib
}

func (ib *ImageBuilder) Build() error {
    for i, step := range ib.steps {
        log.Infof("Executing step %d: %s", i+1, step.Name())
        if err := step.Execute(ib.config); err != nil {
            return fmt.Errorf("build step %d failed: %w", i+1, err)
        }
    }
    return nil
}
```

### 12.2 Package Management

```go
// Consistent interface for different package managers
type PackageManager interface {
    InstallPackages(packages []string) error
    UpdateRepository() error
    CleanCache() error
}

type RpmManager struct {
    chrootPath string
    cacheDir   string
}

func (rm *RpmManager) InstallPackages(packages []string) error {
    for _, pkg := range packages {
        if err := rm.installSinglePackage(pkg); err != nil {
            return fmt.Errorf("failed to install package %s: %w", pkg, err)
        }
    }
    return nil
}