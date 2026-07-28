// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net"

	"github.com/open-edge-platform/image-composer-tool/internal/api"
	"github.com/spf13/cobra"
)

var (
	serveHost      string
	servePort      string
	serveTemplates string
	serveBinary    string
	serveWorkDir   string
	serveSudo      bool
	serveManifest  string
)

// createServeCommand creates the `serve` subcommand that runs the web UI API.
func createServeCommand() *cobra.Command {
	serveCmd := &cobra.Command{
		Use:   "serve [flags]",
		Short: "Run the web UI backend API server",
		Long: `Start the HTTP API that backs the ICT web UI.

Serves the configuration manifest, resolves pre-authored templates, and triggers
image builds via the image-composer-tool binary with streaming build logs.`,
		RunE: executeServe,
	}

	serveCmd.Flags().StringVar(&serveHost, "host", "127.0.0.1",
		"Address to bind. Defaults to localhost only; set 0.0.0.0 to expose on all "+
			"interfaces (not recommended — this API can trigger privileged builds).")
	serveCmd.Flags().StringVarP(&servePort, "port", "p", "8080", "Port to listen on")
	serveCmd.Flags().StringVar(&serveTemplates, "templates-dir", "image-templates", "Directory of pre-authored templates")
	serveCmd.Flags().StringVar(&serveBinary, "ict-binary", "",
		"Path to the image-composer-tool binary used for builds. "+
			"If empty, auto-detects ./build/image-composer-tool, then ./image-composer-tool, then $PATH.")
	serveCmd.Flags().StringVar(&serveWorkDir, "work-dir", "webui-workspace", "Base directory for per-build work/output directories")
	serveCmd.Flags().BoolVar(&serveSudo, "sudo", false,
		"Run builds under `sudo -n` (ICT requires root for chroot/mount). "+
			"Grant scoped, passwordless sudoers rules for the ICT binary only — do not "+
			"give the service blanket sudo. Two rules are needed:\n"+
			"  <svc-user> ALL=(root) NOPASSWD: /path/to/image-composer-tool build *\n"+
			"  <svc-user> ALL=(root) NOPASSWD: /usr/bin/kill -TERM -[0-9]*\n"+
			"The second rule lets the (non-root) server cancel a build by signalling the "+
			"root-owned build process group; without it, cancellation cannot deliver "+
			"SIGTERM across the sudo boundary. Adjust the kill path (e.g. /bin/kill) to "+
			"your distro. SECURITY: the pgid isn't known ahead of time, so sudoers "+
			"cannot scope it — this rule grants the service user root SIGTERM to ANY "+
			"process group, including `kill -TERM -1` (every process on the host). The "+
			"signal is limited to TERM. Accept this deliberately, or run the server as "+
			"root on an isolated build host (no kill rule needed), or omit the rule and "+
			"accept that Cancel reports a cancellation-failure. See web/README.md.")
	serveCmd.Flags().StringVar(&serveManifest, "manifest", "",
		"Path to a manifest YAML to read from disk (live-editable, no rebuild). "+
			"When empty, the manifest embedded at build time is used.")

	return serveCmd
}

func executeServe(cmd *cobra.Command, args []string) error {
	srv, err := api.New(api.Config{
		// net.JoinHostPort brackets IPv6 hosts correctly (e.g. [::1]:8080).
		Addr:         net.JoinHostPort(serveHost, servePort),
		TemplatesDir: serveTemplates,
		ICTBinary:    serveBinary,
		WorkDir:      serveWorkDir,
		Sudo:         serveSudo,
		ManifestPath: serveManifest,
	})
	if err != nil {
		return err
	}
	return srv.Start()
}
