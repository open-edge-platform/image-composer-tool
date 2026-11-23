// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

// Capabilities lists what the current user may do. The UI shows a feature only
// when its capability is true.
type Capabilities struct {
	Authoring bool
}

// Session returns the current user's capabilities. There are no users yet, so
// every caller gets what the server settings grant.
func (s *Service) Session() Capabilities {
	return Capabilities{Authoring: s.cfg.Authoring}
}
