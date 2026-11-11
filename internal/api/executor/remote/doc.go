// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package remote implements service.Executor over the ICT remote build
// protocol (api/v1/remote-build-protocol.yaml): builds are submitted to a
// build service that runs `image-composer-tool build` elsewhere and streams
// progress back. ICT is the client; the service is any server that speaks the
// protocol. The package adds no dependency to the module.
//
// docs/user-guide/architecture/remote-build-protocol.md explains the protocol
// with sequence diagrams and a state diagram of the build lifecycle.
package remote
