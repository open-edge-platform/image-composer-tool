// Package cache provides embedding cache management with content-hash based invalidation.
package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache manages embedding vectors with content-hash based invalidation.
type Cache struct {
	dir        string
	index      *CacheIndex
	mu         sync.RWMutex
	indexPath  string
	vectorsDir string
}

// CacheIndex tracks all cached embeddings.
type CacheIndex struct {
	// ModelID is the embedding model used (e.g., "nomic-embed-text")
	ModelID string `json:"model_id"`

	// Dimensions is the vector size (model-specific: 768, 1536, or 3072)
	Dimensions int `json:"dimensions"`

	// CreatedAt is when the cache was first created
	CreatedAt time.Time `json:"created_at"`

	// Entries maps content hash to cache entry metadata
	Entries map[string]CacheEntry `json:"entries"`
}

// CacheEntry represents metadata for a single cached embedding.
type CacheEntry struct {
	// Source is the human-readable source label (e.g. template filename, bundle id, package name)
	Source string `json:"source"`

	// Kind is the entry type ("template", "bundle", "package")
	Kind string `json:"kind"`

	// ContentHash is the SHA256 hash of the template file content
	ContentHash string `json:"content_hash"`

	// UpdatedAt is when the embedding was last updated
	UpdatedAt time.Time `json:"updated_at"`
}

// PutRequest groups all parameters for Cache.Put.
type PutRequest struct {
	ContentHash string
	ModelID     string
	Source      string // human-readable source label (e.g. template filename, bundle id)
	Kind        string // "template" | "bundle" | "package"
	Dimensions  int    // vector size (optional; defaults to len(Embedding) if <= 0)
	Embedding   []float32
}

// NewCache creates a new cache instance.
func NewCache(dir string) (*Cache, error) {
	vectorsDir := filepath.Join(dir, "embeddings", "vectors")
	if err := os.MkdirAll(vectorsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create vectors directory: %w", err)
	}

	cache := &Cache{
		dir:        dir,
		indexPath:  filepath.Join(dir, "embeddings", "index.json"),
		vectorsDir: vectorsDir,
	}

	// Try to load existing index
	if err := cache.loadIndex(); err != nil {
		// Create new empty index if loading fails
		cache.index = &CacheIndex{
			CreatedAt: time.Now(),
			Entries:   make(map[string]CacheEntry),
		}
	}

	return cache, nil
}

// loadIndex loads the cache index from disk.
func (c *Cache) loadIndex() error {
	data, err := os.ReadFile(c.indexPath)
	if err != nil {
		return err
	}

	var index CacheIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return err
	}

	c.index = &index
	return nil
}

// saveIndex saves the cache index to disk.
func (c *Cache) saveIndex() error {
	data, err := json.MarshalIndent(c.index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	if err := os.WriteFile(c.indexPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

// ComputeContentHash computes SHA256 hash of file content, returning first 16 chars.
func ComputeContentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])[:16]
}

// Get retrieves a cached embedding if it exists and model matches.
func (c *Cache) Get(contentHash, modelID string) ([]float32, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check if model ID matches
	if c.index.ModelID != "" && c.index.ModelID != modelID {
		return nil, false
	}

	// Check if entry exists and has new schema (Source != "")
	entry, exists := c.index.Entries[contentHash]
	if !exists || entry.Source == "" {
		return nil, false
	}

	// Load embedding from disk
	vectorPath := filepath.Join(c.vectorsDir, contentHash+".bin")
	embedding, err := loadEmbedding(vectorPath)
	if err != nil {
		// Entry exists but file is missing, consider it a cache miss
		return nil, false
	}

	_ = entry // entry is used for metadata, embedding loaded from file
	return embedding, true
}

// Put stores an embedding in the cache.
func (c *Cache) Put(req PutRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	dimensions := req.Dimensions
	if dimensions <= 0 {
		dimensions = len(req.Embedding)
	}

	// Update model info if not set or if it changed
	if c.index.ModelID == "" {
		c.index.ModelID = req.ModelID
		c.index.Dimensions = dimensions
	} else if c.index.ModelID != req.ModelID {
		// Model changed, clear entire cache
		if err := c.clearUnsafe(); err != nil {
			return fmt.Errorf("failed to clear cache for model change: %w", err)
		}
		c.index.ModelID = req.ModelID
		c.index.Dimensions = dimensions
	}

	// Save embedding to disk
	vectorPath := filepath.Join(c.vectorsDir, req.ContentHash+".bin")
	if err := saveEmbedding(vectorPath, req.Embedding); err != nil {
		return fmt.Errorf("failed to save embedding: %w", err)
	}

	// Update index
	c.index.Entries[req.ContentHash] = CacheEntry{
		Source:      req.Source,
		Kind:        req.Kind,
		ContentHash: req.ContentHash,
		UpdatedAt:   time.Now(),
	}

	// Persist index
	if err := c.saveIndex(); err != nil {
		return fmt.Errorf("failed to save index: %w", err)
	}

	return nil
}

// clearUnsafe clears all cache entries (caller must hold lock).
func (c *Cache) clearUnsafe() error {
	// Remove all vector files
	entries, err := os.ReadDir(c.vectorsDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	for _, entry := range entries {
		if err := os.Remove(filepath.Join(c.vectorsDir, entry.Name())); err != nil {
			return err
		}
	}

	// Reset index
	c.index = &CacheIndex{
		CreatedAt: time.Now(),
		Entries:   make(map[string]CacheEntry),
	}

	return c.saveIndex()
}

// Clear clears the entire cache.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clearUnsafe()
}

// Stats returns cache statistics.
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var totalSize int64
	entries, _ := os.ReadDir(c.vectorsDir)
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil {
			totalSize += info.Size()
		}
	}

	return CacheStats{
		EntryCount: len(c.index.Entries),
		TotalSize:  totalSize,
		ModelID:    c.index.ModelID,
		Dimensions: c.index.Dimensions,
		CreatedAt:  c.index.CreatedAt,
	}
}

// CacheStats holds cache statistics.
type CacheStats struct {
	EntryCount int
	TotalSize  int64
	ModelID    string
	Dimensions int
	CreatedAt  time.Time
}

// loadEmbedding loads an embedding vector from a binary file.
func loadEmbedding(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data)%4 != 0 {
		return nil, fmt.Errorf("invalid embedding file size: %d bytes", len(data))
	}

	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		bits := binary.LittleEndian.Uint32(data[i*4 : (i+1)*4])
		embedding[i] = math.Float32frombits(bits)
	}

	return embedding, nil
}

// saveEmbedding saves an embedding vector to a binary file.
func saveEmbedding(path string, embedding []float32) error {
	data := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(data[i*4:(i+1)*4], bits)
	}

	return os.WriteFile(path, data, 0644)
}
