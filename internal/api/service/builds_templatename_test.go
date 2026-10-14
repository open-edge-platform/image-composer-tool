// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import "testing"

func TestPostedTemplateName(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "derives from image.name",
			yaml: "image:\n  name: minimal-desktop-ubuntu\n  version: \"24.04\"\n",
			want: "minimal-desktop-ubuntu.yml",
		},
		{
			name: "no image block",
			yaml: "target:\n  os: ubuntu\n",
			want: "template.yml",
		},
		{
			name: "empty name",
			yaml: "image:\n  name: \"\"\n",
			want: "template.yml",
		},
		{
			name: "unparseable document",
			yaml: "image:\n\tname: tabs-are-invalid\n",
			want: "template.yml",
		},
		// The name lands in a Content-Disposition filename, so anything the
		// schema's image.name pattern rejects must not reach it.
		{
			name: "path traversal rejected",
			yaml: "image:\n  name: ../../etc/passwd\n",
			want: "template.yml",
		},
		{
			name: "quote rejected",
			yaml: "image:\n  name: 'evil\"; drop'\n",
			want: "template.yml",
		},
		{
			name: "crlf rejected",
			yaml: "image:\n  name: \"a\\r\\nX-Injected: 1\"\n",
			want: "template.yml",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := postedTemplateName(c.yaml); got != c.want {
				t.Errorf("postedTemplateName() = %q, want %q", got, c.want)
			}
		})
	}
}
