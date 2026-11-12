// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// Protocol is the protocol version this client speaks.
const Protocol = "1"

// Info is what GET /v1/info returns.
type Info struct {
	Name          string          `json:"name"`
	Protocol      string          `json:"protocol"`
	MaxConcurrent int             `json:"maxConcurrent"`
	Phases        []service.Phase `json:"phases,omitempty"`
}

// submission is the body of POST /v1/builds.
type submission struct {
	ID       string            `json:"id"`
	Template string            `json:"template"`
	Files    []file            `json:"files,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type file struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"contentBase64"`
	Mode          string `json:"mode,omitempty"`
}

type buildState struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Phase     *service.Phase    `json:"phase,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	Error     string            `json:"error,omitempty"`
	Artifacts []artifact        `json:"artifacts,omitempty"`
}

type artifact struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	URL       string `json:"url"`
}

type logEvent struct {
	N    int64  `json:"n"`
	Line string `json:"line"`
}

type metaEvent struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type doneEvent struct {
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	Artifacts []artifact `json:"artifacts,omitempty"`
}

// terminalStatus maps a protocol status to the service's, reporting false for a
// build that is still going.
func terminalStatus(s string) (service.BuildStatus, bool) {
	switch s {
	case "succeeded":
		return service.StatusSuccess, true
	case "failed":
		return service.StatusFailed, true
	case "cancelled":
		return service.StatusCancelled, true
	default:
		return "", false
	}
}

func toArtifacts(in []artifact) []service.Artifact {
	out := make([]service.Artifact, 0, len(in))
	for _, a := range in {
		typ := a.Type
		if typ == "" {
			typ = "other"
		}
		out = append(out, service.Artifact{
			Name:   a.Name,
			Type:   typ,
			URL:    a.URL,
			SHA256: a.SHA256,
			Size:   humanSize(a.SizeBytes),
		})
	}
	return out
}

// humanSize renders bytes the way ICT's own output does ("1.13 GB"), empty for zero.
func humanSize(n int64) string {
	if n <= 0 {
		return ""
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.2f %s", f, units[i])
}

// imageName reads image.name from a template, "" when absent.
func imageName(tmpl string) string {
	var doc struct {
		Image struct {
			Name string `yaml:"name"`
		} `yaml:"image"`
	}
	if yaml.Unmarshal([]byte(tmpl), &doc) != nil {
		return ""
	}
	return doc.Image.Name
}
