// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/open-edge-platform/image-composer-tool/internal/api"
	"github.com/open-edge-platform/image-composer-tool/internal/api/auth"
	"github.com/open-edge-platform/image-composer-tool/internal/api/executor/remote"
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
	servePackageRepos string
	serveEdgePack     string
	servePrintSudoers bool

	// Build backend. "local" spawns --ict-binary on this host; "remote" sends
	// each build to a service speaking the remote build protocol
	// (api/v1/remote-build-protocol.yaml), whose capacity then sets how many
	// builds run at once.
	serveExecutor      string
	serveExecutorURL   string
	serveExecutorToken string
	serveMaxConcurrent int

	// Identity. "none" is one fixed local user; "trusted-header" takes the user
	// from headers an authenticating reverse proxy sets in front of this server.
	serveAuth            string
	serveAuthUser        string
	serveAuthUserHeader  string
	serveAuthEmailHeader string
	serveAuthNameHeader  string
	serveAuthGroupHeader string
	serveAuthProxies     string
	serveAuthAllowRemote bool
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
	serveCmd.Flags().StringVar(&servePackageRepos, "package-repos", "",
		"Path to a package-repository catalog YAML to read from disk (live-editable, "+
			"no rebuild). Backs the Advanced tab's repository picker. When empty, the "+
			"catalog embedded at build time is used.")
	serveCmd.Flags().StringVar(&serveEdgePack, "edge-pack", "",
		"Path to an Edge Pack catalog YAML to read from disk (live-editable, no "+
			"rebuild). Backs the Advanced tab's Edge Pack tab, which groups a subset "+
			"of the repository packages by capability domain. When empty, the catalog "+
			"embedded at build time is used.")
	serveCmd.Flags().BoolVar(&servePrintSudoers, "print-sudoers", false,
		"Print the scoped sudoers drop-in required for `--sudo` cancellation and "+
			"artifact reads, then exit. The rules are generated for the current user, "+
			"the resolved --ict-binary, and --work-dir. Install with:\n"+
			"  image-composer-tool serve --print-sudoers | sudo tee /etc/sudoers.d/"+service.SudoersDropInName+"\n"+
			"or run scripts/install-sudoers.sh, which validates with visudo first.")

	addExecutorFlags(serveCmd)
	addAuthFlags(serveCmd)

	return serveCmd
}

// addExecutorFlags registers the build-backend flags. Each defaults to the
// matching env var so a deployment can configure the server without exposing
// values on the command line.
func addExecutorFlags(serveCmd *cobra.Command) {
	serveCmd.Flags().StringVar(&serveExecutor, "executor", envOr("ICT_EXECUTOR", "local"),
		"Where builds run: local spawns --ict-binary on this host; remote sends "+
			"each build to the service at --executor-url, which speaks the remote build "+
			"protocol (api/v1/remote-build-protocol.yaml) and may run several at once. "+
			"env: ICT_EXECUTOR")
	serveCmd.Flags().StringVar(&serveExecutorURL, "executor-url", os.Getenv("ICT_EXECUTOR_URL"),
		"Base URL of the remote build service (env: ICT_EXECUTOR_URL). Required with --executor remote.")
	serveCmd.Flags().StringVar(&serveExecutorToken, "executor-token", os.Getenv("ICT_EXECUTOR_TOKEN"),
		"Bearer token for the remote build service (env: ICT_EXECUTOR_TOKEN). "+
			"Prefer the env var; the flag is visible in ps.")
	serveCmd.Flags().IntVar(&serveMaxConcurrent, "max-concurrent", envInt("ICT_MAX_CONCURRENT", 0),
		"How many builds may run at once (env: ICT_MAX_CONCURRENT). Local builds share "+
			"this host's mounts and loop devices, so the local executor is always 1. "+
			"A remote service reports its own capacity; set this only to lower it.")
}

// addAuthFlags registers the identity flags. See
// docs/user-guide/architecture/serve-behind-a-login-proxy.md.
func addAuthFlags(serveCmd *cobra.Command) {
	serveCmd.Flags().StringVar(&serveAuth, "auth", envOr("ICT_AUTH", "none"),
		"How callers are identified (env: ICT_AUTH). none: every request is one "+
			"fixed user (--default-user) and sees all history, right for a developer's "+
			"own machine. trusted-header: an authenticating reverse proxy in front of "+
			"this server (an OIDC proxy, say) performs the login and passes the user in "+
			"request headers; builds are attributed to that user and history is per "+
			"user. The proxy must be the only way to reach this server: keep --host on "+
			"loopback inside the same pod or host, or anyone able to send a request can "+
			"claim any identity.")
	serveCmd.Flags().StringVar(&serveAuthUser, "default-user", envOr("ICT_DEFAULT_USER", "local"),
		"Identity every request is attributed to with --auth none (env: ICT_DEFAULT_USER).")
	serveCmd.Flags().StringVar(&serveAuthUserHeader, "auth-user-header",
		envOr("ICT_AUTH_USER_HEADER", "X-Forwarded-User"),
		"Header carrying the user's stable id with --auth trusted-header; required on every API request.")
	serveCmd.Flags().StringVar(&serveAuthNameHeader, "auth-name-header",
		envOr("ICT_AUTH_NAME_HEADER", "X-Forwarded-Preferred-Username"),
		"Header carrying the user's display name (optional).")
	serveCmd.Flags().StringVar(&serveAuthEmailHeader, "auth-email-header",
		envOr("ICT_AUTH_EMAIL_HEADER", "X-Forwarded-Email"),
		"Header carrying the user's email (optional).")
	serveCmd.Flags().StringVar(&serveAuthGroupHeader, "auth-groups-header",
		envOr("ICT_AUTH_GROUPS_HEADER", "X-Forwarded-Groups"),
		"Header carrying the user's groups, comma-separated (optional).")
	serveCmd.Flags().StringVar(&serveAuthProxies, "auth-trusted-proxies", os.Getenv("ICT_AUTH_TRUSTED_PROXIES"),
		"Comma-separated addresses or CIDRs the identity headers are accepted from "+
			"(env: ICT_AUTH_TRUSTED_PROXIES). Empty accepts them from any peer, which is "+
			"only safe on a loopback bind.")
	serveCmd.Flags().BoolVar(&serveAuthAllowRemote, "auth-allow-remote-bind", false,
		"Allow --auth trusted-header on a non-loopback --host without --auth-trusted-proxies. "+
			"Only when a network policy guarantees that nothing but the proxy can reach this port.")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

// buildExecutor returns the configured build backend and the concurrency it
// supports; nil means the local ICT binary.
func buildExecutor(cmd *cobra.Command) (service.Executor, int, error) {
	switch serveExecutor {
	case "local", "":
		if serveMaxConcurrent > 1 {
			return nil, 0, errors.New("--max-concurrent above 1 needs --executor remote: " +
				"local builds share this host's mounts and loop devices")
		}
		return nil, 1, nil
	case "remote":
		if serveExecutorURL == "" {
			return nil, 0, errors.New("--executor remote needs --executor-url (or ICT_EXECUTOR_URL)")
		}
		ex, err := remote.New(cmd.Context(), remote.Config{
			URL:          serveExecutorURL,
			Token:        serveExecutorToken,
			TemplatesDir: serveTemplates,
		})
		if err != nil {
			return nil, 0, err
		}
		n := ex.MaxConcurrent()
		if serveMaxConcurrent > 0 && serveMaxConcurrent < n {
			n = serveMaxConcurrent
		}
		return ex, n, nil
	default:
		return nil, 0, fmt.Errorf("unknown --executor %q: use local or remote", serveExecutor)
	}
}

// identityProvider returns the configured auth provider, refusing a
// trusted-header setup that would accept identity headers from anyone.
func identityProvider() (auth.Provider, error) {
	switch serveAuth {
	case "none", "":
		return auth.None{User: serveAuthUser}, nil
	case "trusted-header":
		proxies, err := auth.ParseCIDRs(serveAuthProxies)
		if err != nil {
			return nil, err
		}
		if len(proxies) == 0 && !serveAuthAllowRemote {
			if ip := net.ParseIP(serveHost); serveHost != "localhost" && (ip == nil || !ip.IsLoopback()) {
				return nil, fmt.Errorf("--auth trusted-header on --host %s would accept identity headers "+
					"from any peer; bind to loopback, set --auth-trusted-proxies, or pass --auth-allow-remote-bind",
					serveHost)
			}
		}
		return auth.TrustedHeader{
			UserHeader:     serveAuthUserHeader,
			NameHeader:     serveAuthNameHeader,
			EmailHeader:    serveAuthEmailHeader,
			GroupsHeader:   serveAuthGroupHeader,
			TrustedProxies: proxies,
		}, nil
	default:
		return nil, fmt.Errorf("unknown --auth %q: use none or trusted-header", serveAuth)
	}
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

	// Builds run as separate processes (under sudo with --sudo), so pass them
	// the configuration file this server loaded, as an absolute path.
	configPath := actualConfigFile
	if configPath != "" {
		abs, err := filepath.Abs(configPath)
		if err != nil {
			return fmt.Errorf("resolving configuration file %s: %w", configPath, err)
		}
		configPath = abs
	}

	executor, maxConcurrent, err := buildExecutor(cmd)
	if err != nil {
		return err
	}
	provider, err := identityProvider()
	if err != nil {
		return err
	}

	srv, err := api.New(api.Config{
		// net.JoinHostPort brackets IPv6 hosts correctly (e.g. [::1]:8080).
		Addr:         net.JoinHostPort(serveHost, servePort),
		TemplatesDir: serveTemplates,
		ICTBinary:    serveBinary,
		WorkDir:      serveWorkDir,
		Sudo:         serveSudo,
		ManifestPath: serveManifest,
		ConfigPath:   configPath,

		PackageReposPath: servePackageRepos,
		EdgePackPath:     serveEdgePack,
		Executor:         executor,
		MaxConcurrent:    maxConcurrent,
		Auth:             provider,
	})
	if err != nil {
		return err
	}
	return srv.Start(cmd.Context())
}
