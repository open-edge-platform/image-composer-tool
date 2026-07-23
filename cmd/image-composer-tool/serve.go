// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net"

	"github.com/open-edge-platform/image-composer-tool/internal/api"
	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
	"github.com/spf13/cobra"
)

var (
	serveHost         string
	servePort         string
	serveTemplates    string
	serveBinary       string
	serveWorkDir      string
	serveSudo         bool
	serveManifest     string
	servePrintSudoers bool
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
			"give the service blanket sudo. Three rules are needed (build, cancel, read):\n"+
			"  <svc-user> ALL=(root) NOPASSWD: /path/to/image-composer-tool build *\n"+
			"  <svc-user> ALL=(root) NOPASSWD: /usr/bin/kill -TERM -[0-9]*\n"+
			"  <svc-user> ALL=(root) NOPASSWD: /usr/bin/cat /path/to/workspace/builds/*\n"+
			"The kill rule lets the (non-root) server cancel a build by signalling the "+
			"root-owned build process group; the cat rule lets it stream root-owned "+
			"build artifacts back to the browser. Without them, cancellation and "+
			"downloads fail across the sudo boundary. Don't hand-write these: run\n"+
			"  image-composer-tool serve --print-sudoers [--ict-binary P] [--work-dir D]\n"+
			"to generate the exact rules for this host, or scripts/install-sudoers.sh to "+
			"generate + visudo-validate + install them in one step. The generator resolves "+
			"kill/cat against sudo's own secure_path — the same path sudo uses at runtime — "+
			"and the server logs the resolved kill path at startup, so a locally-installed "+
			"/usr/local/bin/kill shadowing the distro one can't silently make a hand-written "+
			"rule fail as 'sudo: a password is required'. SECURITY: the pgid "+
			"isn't known ahead of time, so the kill rule grants the service user root "+
			"SIGTERM (only TERM) to ANY process group. Accept this deliberately, or run "+
			"the server as root on an isolated build host (no rules needed). "+
			"See web/README.md.")
	serveCmd.Flags().StringVar(&serveManifest, "manifest", "",
		"Path to a manifest YAML to read from disk (live-editable, no rebuild). "+
			"When empty, the manifest embedded at build time is used.")
	serveCmd.Flags().BoolVar(&servePrintSudoers, "print-sudoers", false,
		"Print the scoped sudoers drop-in required for `--sudo` cancellation and "+
			"artifact reads, then exit. The rules are generated for the current user, "+
			"the resolved --ict-binary, and --work-dir. Install with:\n"+
			"  image-composer-tool serve --print-sudoers | sudo tee /etc/sudoers.d/"+service.SudoersDropInName+"\n"+
			"or run scripts/install-sudoers.sh, which validates with visudo first.")

	return serveCmd
}

func executeServe(cmd *cobra.Command, args []string) error {
	// --print-sudoers is a setup helper: emit the scoped drop-in for this host and
	// exit without starting the server. Printed to stdout so it can be piped to
	// `sudo tee /etc/sudoers.d/...`; diagnostics (if any) go to stderr via the
	// returned error.
	if servePrintSudoers {
		spec, err := service.ResolveSudoersSpec(serveBinary, serveWorkDir)
		if err != nil {
			return fmt.Errorf("generating sudoers rules: %w", err)
		}
		fmt.Print(spec.Render())
		return nil
	}

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
