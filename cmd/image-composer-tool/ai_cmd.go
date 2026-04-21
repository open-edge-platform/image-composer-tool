package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/ai"
	"github.com/open-edge-platform/image-composer-tool/internal/ai/rag"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/spf13/cobra"
)

var (
	aiProvider     string
	aiTemplatesDir string
	aiClearCache   bool
	aiCacheStats   bool
	aiSearchOnly   bool
	aiOutput       string
)

func createAICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai [query]",
		Short: "AI-powered template generation using RAG",
		Long: `Generate OS image templates using AI with Retrieval-Augmented Generation (RAG).

The AI command uses semantic search to find relevant template examples and generates
new templates based on natural language descriptions.

Examples:
  # Generate a template from a description
  image-composer-tool ai "create a minimal edge image for elxr with docker support"

  # Generate and save to image-templates/ with a name
  image-composer-tool ai "create a minimal edge image" --output my-custom-image

  # Generate and save to a custom path
  image-composer-tool ai "create an edge image" --output /path/to/output.yml

  # Search for relevant templates without generating
  image-composer-tool ai --search-only "cloud deployment with monitoring"

  # Clear the embedding cache
  image-composer-tool ai --clear-cache

  # Show cache statistics
  image-composer-tool ai --cache-stats

Configuration:
  The AI feature uses sensible defaults. If Ollama is running locally, no configuration
  is needed. For OpenAI, set the OPENAI_API_KEY environment variable.

  Optional configuration in image-composer-tool.yml:
    ai:
      provider: ollama  # or "openai"
      templates_dir: ./image-templates
`,
		Args: cobra.MaximumNArgs(1),
		RunE: runAICommand,
	}

	cmd.Flags().StringVar(&aiProvider, "provider", "", "AI provider: ollama or openai (default: ollama)")
	cmd.Flags().StringVar(&aiTemplatesDir, "templates-dir", "", "Directory containing template files")
	cmd.Flags().BoolVar(&aiClearCache, "clear-cache", false, "Clear the embedding cache")
	cmd.Flags().BoolVar(&aiCacheStats, "cache-stats", false, "Show cache statistics")
	cmd.Flags().BoolVar(&aiSearchOnly, "search-only", false, "Only search for templates, don't generate")
	cmd.Flags().StringVar(&aiOutput, "output", "", "Save generated template to file (name saves to image-templates/<name>.yml, path saves to exact location)")

	return cmd
}

func runAICommand(cmd *cobra.Command, args []string) error {
	log := logger.Logger()

	// Build configuration
	config := ai.DefaultConfig()

	// Apply command-line overrides
	if aiProvider != "" {
		config.Provider = ai.ProviderType(aiProvider)
	}
	if aiTemplatesDir != "" {
		config.TemplatesDir = aiTemplatesDir
	}

	// Handle cache-stats command
	if aiCacheStats {
		return showCacheStats(config)
	}

	// Handle clear-cache command
	if aiClearCache {
		return clearCache(config)
	}

	// Require a query for search/generate
	if len(args) == 0 {
		return fmt.Errorf("query is required for template search/generation")
	}
	query := args[0]

	// Create RAG engine
	log.Info("Initializing AI engine...")
	engine, err := rag.NewEngine(config)
	if err != nil {
		return fmt.Errorf("failed to create AI engine: %w", err)
	}

	// Initialize (index templates)
	ctx := context.Background()
	log.Info("Indexing templates...")
	if err := engine.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize AI engine: %w", err)
	}

	stats := engine.GetStats()
	log.Infof("Indexed %d templates using %s provider", stats.TemplateCount, stats.Provider)

	if aiSearchOnly {
		return runSearch(engine, query)
	}

	return runGenerate(engine, query, config.TemplatesDir)
}

func runSearch(engine *rag.Engine, query string) error {
	log := logger.Logger()
	ctx := context.Background()

	log.Infof("Searching for: %s", query)

	results, err := engine.Search(ctx, query)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No matching templates found.")
		return nil
	}

	fmt.Printf("\nFound %d matching templates:\n\n", len(results))
	for i, result := range results {
		fmt.Printf("%d. %s\n", i+1, result.Template.FileName)
		fmt.Printf("   Score: %.2f (semantic: %.2f, keyword: %.2f, package: %.2f)\n",
			result.Score, result.SemanticScore, result.KeywordScore, result.PackageScore)
		if result.Template.Metadata.Description != "" {
			fmt.Printf("   Description: %s\n", result.Template.Metadata.Description)
		}
		fmt.Printf("   Distribution: %s, Architecture: %s, Type: %s\n",
			result.Template.Distribution, result.Template.Architecture, result.Template.ImageType)
		if len(result.Template.Packages) > 0 {
			fmt.Printf("   Packages: %v\n", result.Template.Packages)
		}
		fmt.Println()
	}

	return nil
}

func runGenerate(engine *rag.Engine, query string, templatesDir string) error {
	log := logger.Logger()
	ctx := context.Background()

	log.Infof("Generating template for: %s", query)

	// First show search results
	results, err := engine.Search(ctx, query)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	// Check early if output path matches any indexed template
	if aiOutput != "" {
		outputPath, err := determineOutputPath(templatesDir)
		if err != nil {
			return fmt.Errorf("failed to determine output path: %w", err)
		}
		if !checkAndConfirmOverwrite(outputPath, results) {
			fmt.Println("Aborted. Please re-run with a different --output name.")
			return nil
		}
	}

	if len(results) > 0 {
		fmt.Printf("\nUsing %d reference templates:\n", min(3, len(results)))
		for i, result := range results {
			if i >= 3 {
				break
			}
			fmt.Printf("  - %s (score: %.2f)\n", result.Template.FileName, result.Score)
		}
		fmt.Println()
	}

	// Generate template
	fmt.Println("Generating template...")
	template, err := engine.Generate(ctx, query)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	fmt.Println("\n--- Generated Template ---")
	fmt.Println(template)
	fmt.Println("--- End Template ---")

	// Determine if we should save the template
	shouldSave := aiOutput != ""

	if shouldSave {
		outputPath, err := determineOutputPath(templatesDir)
		if err != nil {
			return fmt.Errorf("failed to determine output path: %w", err)
		}

		// Ensure directory exists
		dir := filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// Write the template
		if err := os.WriteFile(outputPath, []byte(template), 0644); err != nil {
			return fmt.Errorf("failed to write template: %w", err)
		}

		fmt.Printf("\n✓ Template saved to: %s\n", outputPath)
	} else {
		fmt.Println("\nTo save this template, use --output <name-or-path>")
	}

	return nil
}

// determineOutputPath determines the output file path based on --output flag
func determineOutputPath(templatesDir string) (string, error) {
	if aiOutput == "" {
		return "", fmt.Errorf("no output path specified")
	}

	// Check if it looks like a file path:
	// - Has a file extension (e.g., .yml, .yaml)
	// - Has a path separator (absolute or relative path like /path or subdir/name)
	// - Starts with ./ or ../ (explicit relative path)
	isPath := filepath.Ext(aiOutput) != "" ||
		filepath.Dir(aiOutput) != "." ||
		strings.HasPrefix(aiOutput, "./") ||
		strings.HasPrefix(aiOutput, "../")

	if isPath {
		// Convert to absolute path for consistency
		absPath, err := filepath.Abs(aiOutput)
		if err != nil {
			return aiOutput, nil // Fall back to original if Abs fails
		}
		// Add .yml extension if not present
		if filepath.Ext(absPath) == "" {
			absPath += ".yml"
		}
		return absPath, nil
	}

	// Otherwise treat it as a name, save to templatesDir/<name>.yml
	return filepath.Join(templatesDir, aiOutput+".yml"), nil
}

func showCacheStats(config ai.Config) error {
	if !config.Cache.Enabled {
		fmt.Println("Cache is disabled.")
		return nil
	}

	indexPath := filepath.Join(config.Cache.Dir, "embeddings", "index.json")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		fmt.Println("Cache is empty (no index file found).")
		return nil
	}

	// Create a temporary engine just to get cache stats
	engine, err := rag.NewEngine(config)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	stats := engine.GetStats()
	if stats.CacheStats == nil {
		fmt.Println("Cache is empty.")
		return nil
	}

	cs := stats.CacheStats
	fmt.Println("Cache Statistics:")
	fmt.Printf("  Entries: %d\n", cs.EntryCount)
	fmt.Printf("  Total Size: %.2f KB\n", float64(cs.TotalSize)/1024)
	fmt.Printf("  Model: %s\n", cs.ModelID)
	fmt.Printf("  Dimensions: %d\n", cs.Dimensions)
	fmt.Printf("  Created: %s\n", cs.CreatedAt.Format("2006-01-02 15:04:05"))

	return nil
}

func clearCache(config ai.Config) error {
	if !config.Cache.Enabled {
		fmt.Println("Cache is disabled.")
		return nil
	}

	engine, err := rag.NewEngine(config)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	if err := engine.ClearCache(); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	fmt.Println("Cache cleared successfully.")
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// checkAndConfirmOverwrite checks if the output path matches any indexed template
// and prompts the user for confirmation. Returns true if user wants to continue.
func checkAndConfirmOverwrite(outputPath string, results []rag.SearchResult) bool {
	if len(results) == 0 {
		return true
	}

	// Get the base name of the output file
	outputBase := filepath.Base(outputPath)

	// Check against search results
	for _, result := range results {
		if result.Template.FileName == outputBase {
			// Get absolute path of the output for comparison
			absOutput, err := filepath.Abs(outputPath)
			if err != nil {
				absOutput = outputPath
			}

			fmt.Printf("\n⚠ Warning: Output file '%s' matches an indexed template that was used as a reference.\n", outputBase)
			fmt.Printf("  This may cause the generated template to influence future generations.\n")
			fmt.Printf("  Consider using a different name or saving to a location outside '%s'.\n\n", filepath.Dir(absOutput))

			// Prompt user for confirmation
			fmt.Print("Do you want to continue anyway? [y/N]: ")
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return false
			}

			response = strings.TrimSpace(strings.ToLower(response))
			return response == "y" || response == "yes"
		}
	}

	return true
}
