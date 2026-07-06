// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/open-edge-platform/image-composer-tool/internal/api"
	"github.com/spf13/cobra"
)

var (
	servePort      string
	serveTemplates string
	serveBinary    string
	serveWorkDir   string
	serveSudo      bool
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

	serveCmd.Flags().StringVarP(&servePort, "port", "p", "8080", "Port to listen on")
	serveCmd.Flags().StringVar(&serveTemplates, "templates-dir", "image-templates", "Directory of pre-authored templates")
	serveCmd.Flags().StringVar(&serveBinary, "ict-binary", "./image-composer-tool", "Path to the image-composer-tool binary used for builds")
	serveCmd.Flags().StringVar(&serveWorkDir, "work-dir", "webui-workspace", "Base directory for per-build work/output directories")
	serveCmd.Flags().BoolVar(&serveSudo, "sudo", false, "Run builds under `sudo -n` (ICT requires root for chroot/mount operations)")

	return serveCmd
}

func executeServe(cmd *cobra.Command, args []string) error {
	srv, err := api.New(api.Config{
		Addr:         ":" + servePort,
		TemplatesDir: serveTemplates,
		ICTBinary:    serveBinary,
		WorkDir:      serveWorkDir,
		Sudo:         serveSudo,
	})
	if err != nil {
		return err
	}
	return srv.Start()
}
