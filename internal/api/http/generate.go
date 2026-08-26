// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package httpapi holds the HTTP contract types and server interface generated
// from the OpenAPI spec at api/v1/openapi-template-builder.yaml. Do not edit
// gen.go by hand — change the spec and regenerate.
//
// The generator is pinned via the `tool` directive in go.mod (oapi-codegen v2),
// so `go generate` uses the module-locked version rather than whatever is on
// PATH. Run `go generate ./internal/api/http` after editing the spec.
package httpapi

//go:generate go tool oapi-codegen -config cfg.yaml ../../../api/v1/openapi-template-builder.yaml
