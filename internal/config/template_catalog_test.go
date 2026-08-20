package config

import (
	"path/filepath"
	"testing"
)

func TestUbuntuTemplateCatalogLoadsAndMerges(t *testing.T) {
	for _, dir := range []string{"ubuntu24", "ubuntu26"} {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			templates, err := filepath.Glob(filepath.Join("..", "..", "image-templates", dir, "*.yml"))
			if err != nil {
				t.Fatalf("glob templates: %v", err)
			}
			if len(templates) == 0 {
				t.Fatalf("no templates found for %s", dir)
			}
			for _, template := range templates {
				template := template
				t.Run(filepath.Base(template), func(t *testing.T) {
					if _, err := LoadAndMergeTemplate(template); err != nil {
						t.Fatalf("LoadAndMergeTemplate(%s): %v", template, err)
					}
				})
			}
		})
	}
}
