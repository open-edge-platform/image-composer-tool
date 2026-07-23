// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	httpapi "github.com/open-edge-platform/image-composer-tool/internal/api/http"
	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// This file maps the service layer's plain domain types to the generated
// OpenAPI contract types (internal/api/http) and back. Keeping the conversions
// here leaves the handlers thin and the service free of HTTP/contract types.

// optStr returns nil for an empty string, else a pointer to it — for optional
// (omitempty) string fields in the generated types.
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// --- inbound: generated request types -> service types ---

func toSelection(r httpapi.ComposeRequest) service.Selection {
	sel := service.Selection{
		Vertical:  r.Vertical,
		Platform:  r.Platform,
		OS:        r.Os,
		ImageType: r.ImageType,
	}
	if r.Sku != nil {
		sel.SKU = *r.Sku
	}
	return sel
}

func toBuildRequest(r httpapi.BuildRequest) service.BuildRequest {
	req := service.BuildRequest{}
	if r.Compose != nil {
		sel := toSelection(*r.Compose)
		req.Compose = &sel
	}
	if r.Yaml != nil {
		req.YAML = *r.Yaml
	}
	return req
}

// --- outbound: service types -> generated response types ---

func fromManifest(m *service.Manifest) httpapi.Manifest {
	out := httpapi.Manifest{
		Combinations: make([]httpapi.Combination, len(m.Combinations)),
		Verticals:    fromOptions(m.Verticals),
		Skus:         fromOptions(m.SKUs),
		Platforms:    fromOptions(m.Platforms),
		Targets:      make([]httpapi.Target, len(m.Targets)),
	}
	for i, c := range m.Combinations {
		out.Combinations[i] = httpapi.Combination{
			Vertical:  c.Vertical,
			Sku:       optStr(c.SKU),
			Platform:  c.Platform,
			Os:        c.OS,
			ImageType: c.ImageType,
			Template:  c.Template,
		}
	}
	for i, t := range m.Targets {
		out.Targets[i] = httpapi.Target{
			Id:          t.ID,
			DisplayName: t.DisplayName,
			Os:          t.OS,
			Arch:        t.Arch,
		}
	}
	return out
}

func fromOptions(opts []service.Option) []httpapi.Option {
	out := make([]httpapi.Option, len(opts))
	for i, o := range opts {
		out[i] = httpapi.Option{Id: o.ID, DisplayName: o.DisplayName}
	}
	return out
}

func fromSummary(s *service.ComposeSummary) *httpapi.ComposeSummary {
	if s == nil {
		return nil
	}
	return &httpapi.ComposeSummary{
		Vertical:       s.Vertical,
		Sku:            s.SKU,
		Platform:       s.Platform,
		Os:             s.OS,
		ImageType:      s.ImageType,
		ImageName:      s.ImageName,
		ImageVersion:   s.ImageVersion,
		Description:    s.Description,
		Architecture:   s.Architecture,
		KernelVersion:  s.KernelVersion,
		PackageCount:   s.PackageCount,
		DiskSize:       s.DiskSize,
		PartitionCount: s.PartitionCount,
		PartitionTable: s.PartitionTable,
		Hostname:       s.Hostname,
		BaseImage:      optStr(s.BaseImage),
	}
}

func fromComposeResult(r *service.ComposeResult) httpapi.ComposeResponse {
	return httpapi.ComposeResponse{
		Template: r.Template,
		Yaml:     r.YAML,
		Summary:  *fromSummary(&r.Summary),
	}
}

func fromBuildAccepted(a *service.BuildAccepted) httpapi.BuildAccepted {
	return httpapi.BuildAccepted{
		BuildId: a.BuildID,
		Status:  string(a.Status),
		LogsUrl: a.LogsURL,
	}
}

func fromArtifact(a service.Artifact) httpapi.Artifact {
	return httpapi.Artifact{
		Name: a.Name,
		Type: httpapi.ArtifactType(a.Type),
		Path: a.Path,
		Size: optStr(a.Size),
	}
}

func fromArtifacts(arts []service.Artifact) []httpapi.Artifact {
	out := make([]httpapi.Artifact, len(arts))
	for i, a := range arts {
		out[i] = fromArtifact(a)
	}
	return out
}

func fromArtifactList(l *service.ArtifactList) httpapi.ArtifactList {
	return httpapi.ArtifactList{
		BuildId:   l.BuildID,
		Status:    string(l.Status),
		Artifacts: fromArtifacts(l.Artifacts),
	}
}

func fromBuildDetails(d *service.BuildDetails) httpapi.BuildDetails {
	return httpapi.BuildDetails{
		BuildId:     d.BuildID,
		Status:      httpapi.BuildDetailsStatus(d.Status),
		Command:     d.Command,
		Template:    d.Template,
		TemplateUrl: d.TemplateURL,
		WorkDir:     d.WorkDir,
		CacheDir:    d.CacheDir,
		Summary:     fromSummary(d.Summary),
		HasLogFile:  d.HasLogFile,
		ErrMsg:      optStr(d.ErrMsg),
	}
}

func fromHistory(items []service.HistoryItem) httpapi.BuildList {
	out := httpapi.BuildList{Builds: make([]httpapi.HistoryItem, len(items))}
	for i, it := range items {
		out.Builds[i] = httpapi.HistoryItem{
			Id:        it.ID,
			Status:    httpapi.HistoryItemStatus(it.Status),
			Template:  it.Template,
			CreatedAt: it.CreatedAt,
			Summary:   fromSummary(it.Summary),
		}
	}
	return out
}
