package provider

import (
	"fmt"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// MockProvider implements the Provider interface for testing
type MockProvider struct {
	NameFunc        func(dist, arch string) string
	InitFunc        func(dist, arch string) error
	PreProcessFunc  func(template *config.ImageTemplate) error
	BuildImageFunc  func(template *config.ImageTemplate) error
	PostProcessFunc func(template *config.ImageTemplate, err error) error
}

// Mock provider implementations
func (m *MockProvider) Name(dist, arch string) string {
	if m.NameFunc != nil {
		return m.NameFunc(dist, arch)
	}
	return fmt.Sprintf("mock-%s-%s", dist, arch)
}

func (m *MockProvider) Init(dist, arch string) error {
	if m.InitFunc != nil {
		return m.InitFunc(dist, arch)
	}
	return nil
}

func (m *MockProvider) PreProcess(template *config.ImageTemplate) error {
	if m.PreProcessFunc != nil {
		return m.PreProcessFunc(template)
	}
	return nil
}

func (m *MockProvider) BuildImage(template *config.ImageTemplate) error {
	if m.BuildImageFunc != nil {
		return m.BuildImageFunc(template)
	}
	return nil
}

func (m *MockProvider) PostProcess(template *config.ImageTemplate, err error) error {
	if m.PostProcessFunc != nil {
		return m.PostProcessFunc(template, err)
	}
	return nil
}

// Helper function to create a test ImageTemplate
func createTestImageTemplate() *config.ImageTemplate {
	return &config.ImageTemplate{
		Image: config.ImageInfo{
			Name:    "test-image",
			Version: "1.0.0",
		},
		Target: config.TargetInfo{
			OS:        "azure-linux",
			Dist:      "azl3",
			Arch:      "amd64",
			ImageType: "qcow2",
		},
		SystemConfig: config.SystemConfig{
			Name:        "test-system",
			Description: "Test system configuration",
			Packages:    []string{"curl", "wget"},
		},
	}
}

// TestProviderInterface tests that MockProvider implements Provider interface
func TestProviderInterface(t *testing.T) {
	var _ Provider = (*MockProvider)(nil) // Compile-time interface check
}

// TestMockProviderName tests the Name method functionality
func TestMockProviderName(t *testing.T) {
	mock := &MockProvider{}

	name := mock.Name("azl3", "amd64")
	expected := "mock-azl3-amd64"

	if name != expected {
		t.Errorf("Expected name %s, got %s", expected, name)
	}
}

// TestMockProviderNameWithCustomFunc tests Name method with custom function
func TestMockProviderNameWithCustomFunc(t *testing.T) {
	mock := &MockProvider{
		NameFunc: func(dist, arch string) string {
			return fmt.Sprintf("custom-%s-%s-provider", dist, arch)
		},
	}

	name := mock.Name("azl3", "amd64")
	expected := "custom-azl3-amd64-provider"

	if name != expected {
		t.Errorf("Expected name %s, got %s", expected, name)
	}
}

// TestMockProviderInit tests the Init method
func TestMockProviderInit(t *testing.T) {
	mock := &MockProvider{}

	err := mock.Init("azl3", "amd64")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestMockProviderInitWithError tests Init method returning error
func TestMockProviderInitWithError(t *testing.T) {
	expectedError := fmt.Errorf("init failed")
	mock := &MockProvider{
		InitFunc: func(dist, arch string) error {
			return expectedError
		},
	}

	err := mock.Init("azl3", "amd64")
	if err != expectedError {
		t.Errorf("Expected error %v, got %v", expectedError, err)
	}
}

// TestMockProviderPreProcess tests the PreProcess method
func TestMockProviderPreProcess(t *testing.T) {
	mock := &MockProvider{}
	template := createTestImageTemplate()

	err := mock.PreProcess(template)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestMockProviderPreProcessWithError tests PreProcess method returning error
func TestMockProviderPreProcessWithError(t *testing.T) {
	expectedError := fmt.Errorf("preprocess failed")
	mock := &MockProvider{
		PreProcessFunc: func(template *config.ImageTemplate) error {
			return expectedError
		},
	}
	template := createTestImageTemplate()

	err := mock.PreProcess(template)
	if err != expectedError {
		t.Errorf("Expected error %v, got %v", expectedError, err)
	}
}

// TestMockProviderBuildImage tests the BuildImage method
func TestMockProviderBuildImage(t *testing.T) {
	mock := &MockProvider{}
	template := createTestImageTemplate()

	err := mock.BuildImage(template)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestMockProviderBuildImageWithError tests BuildImage method returning error
func TestMockProviderBuildImageWithError(t *testing.T) {
	expectedError := fmt.Errorf("build failed")
	mock := &MockProvider{
		BuildImageFunc: func(template *config.ImageTemplate) error {
			return expectedError
		},
	}
	template := createTestImageTemplate()

	err := mock.BuildImage(template)
	if err != expectedError {
		t.Errorf("Expected error %v, got %v", expectedError, err)
	}
}

// TestMockProviderPostProcess tests the PostProcess method
func TestMockProviderPostProcess(t *testing.T) {
	mock := &MockProvider{}
	template := createTestImageTemplate()
	inputError := fmt.Errorf("some previous error")

	err := mock.PostProcess(template, inputError)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestMockProviderPostProcessWithError tests PostProcess method returning error
func TestMockProviderPostProcessWithError(t *testing.T) {
	expectedError := fmt.Errorf("postprocess failed")
	mock := &MockProvider{
		PostProcessFunc: func(template *config.ImageTemplate, err error) error {
			return expectedError
		},
	}
	template := createTestImageTemplate()
	inputError := fmt.Errorf("some previous error")

	err := mock.PostProcess(template, inputError)
	if err != expectedError {
		t.Errorf("Expected error %v, got %v", expectedError, err)
	}
}

// TestProviderRegistry tests the provider registration and retrieval system
func TestProviderRegistry(t *testing.T) {
	// Save original registry and restore after test
	originalProviders := make(map[string]Provider)
	for k, v := range providers {
		originalProviders[k] = v
	}
	defer func() {
		providers = originalProviders
	}()

	// Clear providers for clean test
	providers = make(map[string]Provider)

	// Create and register mock provider
	mock := &MockProvider{
		NameFunc: func(dist, arch string) string {
			return "test-provider-azl3-amd64"
		},
	}

	Register(mock, "azl3", "amd64")

	// Test retrieval
	retrieved, exists := Get("test-provider-azl3-amd64")
	if !exists {
		t.Fatal("Expected provider to exist after registration")
	}

	if retrieved != mock {
		t.Error("Expected retrieved provider to be the same instance as registered")
	}

	// Test Name method on retrieved provider
	name := retrieved.Name("azl3", "amd64")
	expected := "test-provider-azl3-amd64"
	if name != expected {
		t.Errorf("Expected name %s, got %s", expected, name)
	}
}

// TestProviderRegistryNonExistent tests retrieving non-existent provider
func TestProviderRegistryNonExistent(t *testing.T) {
	// Save original registry and restore after test
	originalProviders := make(map[string]Provider)
	for k, v := range providers {
		originalProviders[k] = v
	}
	defer func() {
		providers = originalProviders
	}()

	// Clear providers for clean test
	providers = make(map[string]Provider)

	// Try to get non-existent provider
	_, exists := Get("non-existent-provider")
	if exists {
		t.Error("Expected non-existent provider to not exist")
	}
}

// TestProviderRegistryMultiple tests registering multiple providers
func TestProviderRegistryMultiple(t *testing.T) {
	// Save original registry and restore after test
	originalProviders := make(map[string]Provider)
	for k, v := range providers {
		originalProviders[k] = v
	}
	defer func() {
		providers = originalProviders
	}()

	// Clear providers for clean test
	providers = make(map[string]Provider)

	// Create and register multiple mock providers
	mock1 := &MockProvider{
		NameFunc: func(dist, arch string) string {
			return "provider1-azl3-amd64"
		},
	}
	mock2 := &MockProvider{
		NameFunc: func(dist, arch string) string {
			return "provider2-emt3-arm64"
		},
	}

	Register(mock1, "azl3", "amd64")
	Register(mock2, "emt3", "arm64")

	// Test retrieval of both providers
	retrieved1, exists1 := Get("provider1-azl3-amd64")
	if !exists1 {
		t.Fatal("Expected provider1 to exist after registration")
	}

	retrieved2, exists2 := Get("provider2-emt3-arm64")
	if !exists2 {
		t.Fatal("Expected provider2 to exist after registration")
	}

	if retrieved1 == retrieved2 {
		t.Error("Expected different provider instances")
	}

	// Verify correct providers were retrieved
	if retrieved1.Name("azl3", "amd64") != "provider1-azl3-amd64" {
		t.Error("Provider1 name mismatch")
	}

	if retrieved2.Name("emt3", "arm64") != "provider2-emt3-arm64" {
		t.Error("Provider2 name mismatch")
	}
}

// TestProviderOverwrite tests overwriting an existing provider
func TestProviderOverwrite(t *testing.T) {
	// Save original registry and restore after test
	originalProviders := make(map[string]Provider)
	for k, v := range providers {
		originalProviders[k] = v
	}
	defer func() {
		providers = originalProviders
	}()

	// Clear providers for clean test
	providers = make(map[string]Provider)

	providerName := "overwrite-test-provider"

	// Create and register first provider
	mock1 := &MockProvider{
		NameFunc: func(dist, arch string) string {
			return providerName
		},
		InitFunc: func(dist, arch string) error {
			return fmt.Errorf("first provider init")
		},
	}
	Register(mock1, "azl3", "amd64")

	// Create and register second provider with same name (overwrite)
	mock2 := &MockProvider{
		NameFunc: func(dist, arch string) string {
			return providerName
		},
		InitFunc: func(dist, arch string) error {
			return fmt.Errorf("second provider init")
		},
	}
	Register(mock2, "azl3", "amd64")

	// Verify second provider overwrote the first
	retrieved, exists := Get(providerName)
	if !exists {
		t.Fatal("Expected provider to exist")
	}

	// Test that it's the second provider by calling Init
	err := retrieved.Init("azl3", "amd64")
	if err == nil || err.Error() != "second provider init" {
		t.Errorf("Expected 'second provider init' error, got %v", err)
	}
}

// TestProviderWorkflow tests a complete provider workflow
func TestProviderWorkflow(t *testing.T) {
	// Save original registry and restore after test
	originalProviders := make(map[string]Provider)
	for k, v := range providers {
		originalProviders[k] = v
	}
	defer func() {
		providers = originalProviders
	}()

	// Clear providers for clean test
	providers = make(map[string]Provider)

	// Track method calls
	var callLog []string

	mock := &MockProvider{
		NameFunc: func(dist, arch string) string {
			return "workflow-test-provider"
		},
		InitFunc: func(dist, arch string) error {
			callLog = append(callLog, fmt.Sprintf("Init(%s, %s)", dist, arch))
			return nil
		},
		PreProcessFunc: func(template *config.ImageTemplate) error {
			callLog = append(callLog, fmt.Sprintf("PreProcess(%s)", template.Image.Name))
			return nil
		},
		BuildImageFunc: func(template *config.ImageTemplate) error {
			callLog = append(callLog, fmt.Sprintf("BuildImage(%s)", template.Image.Name))
			return nil
		},
		PostProcessFunc: func(template *config.ImageTemplate, err error) error {
			callLog = append(callLog, fmt.Sprintf("PostProcess(%s, %v)", template.Image.Name, err))
			return nil
		},
	}

	// Register provider
	Register(mock, "azl3", "amd64")

	// Retrieve and execute workflow
	provider, exists := Get("workflow-test-provider")
	if !exists {
		t.Fatal("Expected provider to exist")
	}

	template := createTestImageTemplate()

	// Execute complete workflow
	if err := provider.Init("azl3", "amd64"); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := provider.PreProcess(template); err != nil {
		t.Fatalf("PreProcess failed: %v", err)
	}

	if err := provider.BuildImage(template); err != nil {
		t.Fatalf("BuildImage failed: %v", err)
	}

	if err := provider.PostProcess(template, nil); err != nil {
		t.Fatalf("PostProcess failed: %v", err)
	}

	// Verify all methods were called in correct order
	expectedCalls := []string{
		"Init(azl3, amd64)",
		"PreProcess(test-image)",
		"BuildImage(test-image)",
		"PostProcess(test-image, <nil>)",
	}

	if len(callLog) != len(expectedCalls) {
		t.Fatalf("Expected %d calls, got %d: %v", len(expectedCalls), len(callLog), callLog)
	}

	for i, expected := range expectedCalls {
		if callLog[i] != expected {
			t.Errorf("Call %d: expected %s, got %s", i, expected, callLog[i])
		}
	}
}
