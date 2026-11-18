package imageprovision

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

const (
	unitDir = "etc/systemd/system"
	// wantsDir is where `systemctl enable` would link a WantedBy=multi-user.target unit.
	wantsDir = unitDir + "/multi-user.target.wants"
	// ProvisioningTriggerUnit starts the script units at boot. The script units
	// are in no target's wants directory: multi-user.target is implicitly
	// ordered after every unit it wants, and cloud-init's cloud-final.service is
	// ordered after multi-user.target, so a wanted unit that is also After=
	// cloud-final.service is an ordering cycle and systemd drops its job.
	ProvisioningTriggerUnit = "ict-provision-start.service"
	// StampDir records completed first-boot scripts on the deployed system.
	StampDir = config.ProvisioningStampDir
	// FailedDir records failed scripts on the deployed system.
	FailedDir = config.ProvisioningFailedDir
)

// noticeDirs are the image directories the failure notices are written to.
var noticeDirs = []string{"etc/issue.d", "etc/motd.d"}

// reportScript is installed at config.ProvisioningReportScript.
//
//go:embed report.sh
var reportScript []byte

// ProvisioningUnitName returns the unit name for the index-th script in
// execution order. The zero-padded index keeps `systemctl list-units`
// sorted in run order.
func ProvisioningUnitName(index int, script config.ProvisioningScript) string {
	return fmt.Sprintf("ict-provision-%03d-%s.service", index, script.Name)
}

// ConfigureProvisioningScripts makes each script executable and installs a
// oneshot unit for it, in the given order; each unit requires the previous one,
// so a failed script stops the scripts after it. A first-boot script's unit
// writes a stamp file only when the script succeeds, so a failed script is
// retried on the next boot; every-boot scripts run on each boot. A failed
// script is also recorded and shown at the login prompt and after login until
// it succeeds. A script with StopOnFailure is not retried at all: its unit fails
// to start on later boots, and blocks the scripts after it, until an
// administrator retries it.
//
// By default the units run after network-online.target and, when cloud-init is
// running, after cloud-final.service, started at boot by an enabled trigger
// unit once the system is up. With beforeLogin the units are wanted by
// multi-user.target and ordered before systemd-user-sessions.service, so the
// login prompt and SSH logins wait for the scripts to finish.
func ConfigureProvisioningScripts(installRoot string, scripts []config.ProvisioningScript, beforeLogin bool) error {
	if len(scripts) == 0 {
		return nil
	}
	return withRoot(installRoot, func(root *os.Root) error {
		if err := root.MkdirAll(wantsDir, 0755); err != nil {
			return fmt.Errorf("creating /%s: %w", wantsDir, err)
		}
		if err := writeFile(root, rel(config.ProvisioningReportScript), reportScript, 0755); err != nil {
			return err
		}
		// The failure notices are written here at runtime. Creating the directories
		// now also fails the build if something already made one a regular file.
		for _, dir := range noticeDirs {
			info, existed := root.Stat(dir)
			if err := root.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating /%s for provisioning failure notices: %w", dir, err)
			}
			// Login programs read the notices as the logging-in user: a directory made
			// here gets 0755 whatever the umask; an existing one keeps its mode.
			if existed != nil {
				if err := root.Chmod(dir, 0755); err != nil {
					return fmt.Errorf("setting the mode of /%s: %w", dir, err)
				}
			} else if perm := info.Mode().Perm(); perm&0o005 != 0o005 {
				log.Warnf("/%s has mode %o, so failure notices are not shown to users other than root", dir, perm)
			}
		}
		previous := ""
		units := make([]string, 0, len(scripts))
		for i, script := range scripts {
			info, err := root.Stat(rel(script.Final))
			if err != nil {
				return fmt.Errorf("provisioning script %s: %s is missing from the image: %w",
					script.Name, script.Final, err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("provisioning script %s: %s is not a regular file in the image "+
					"(final must be the script's own path, not a directory)", script.Name, script.Final)
			}
			if err := root.Chmod(rel(script.Final), 0755); err != nil {
				return fmt.Errorf("provisioning script %s: making %s executable: %w",
					script.Name, script.Final, err)
			}
			unit := ProvisioningUnitName(i, script)
			unitFile := []byte(renderProvisioningUnit(script, previous, beforeLogin))
			if err := writeFile(root, unitDir+"/"+unit, unitFile, 0644); err != nil {
				return err
			}
			if beforeLogin {
				if err := enableUnit(root, unit); err != nil {
					return err
				}
			}
			units = append(units, unit)
			previous = unit
		}
		if !beforeLogin {
			triggerFile := []byte(renderProvisioningTrigger(units))
			if err := writeFile(root, unitDir+"/"+ProvisioningTriggerUnit, triggerFile, 0644); err != nil {
				return err
			}
			if err := enableUnit(root, ProvisioningTriggerUnit); err != nil {
				return err
			}
		}
		log.Infof("Installed %d boot-time provisioning unit(s)", len(scripts))
		return nil
	})
}

func isFirstBoot(script config.ProvisioningScript) bool {
	return script.Stage != config.ProvisioningStageEveryBoot
}

func renderProvisioningUnit(script config.ProvisioningScript, after string, beforeLogin bool) string {
	var b strings.Builder
	b.WriteString("# Generated by image-composer-tool\n[Unit]\n")
	writeUnitSection(&b, script, after, beforeLogin)
	b.WriteString("\n[Service]\nType=oneshot\nRemainAfterExit=yes\n")
	writeServiceSection(&b, script, beforeLogin)
	if beforeLogin {
		b.WriteString("\n[Install]\nWantedBy=multi-user.target\n")
	}
	return b.String()
}

// writeUnitSection writes the [Unit] body: ordering, dependencies and the
// first-boot stamp condition.
func writeUnitSection(b *strings.Builder, script config.ProvisioningScript, after string, beforeLogin bool) {
	if beforeLogin {
		fmt.Fprintf(b, "Description=ICT provisioning script %s (logins are unavailable until it completes)\n", script.Name)
		// cloud-final.service is After=multi-user.target, so a unit ordered
		// after it cannot also be before systemd-user-sessions.service (an
		// ordering cycle); cloud-config.service is the last cloud-init stage
		// that is not. The apt timers are kept off until the script is done
		// so they cannot hold the dpkg lock it needs.
		b.WriteString("Wants=network-online.target\nAfter=network-online.target cloud-config.service")
	} else {
		fmt.Fprintf(b, "Description=ICT provisioning script %s\n", script.Name)
		// cloud-final.service is where cloud-init applies packages, users, and
		// runcmd, so scripts must wait for it. Ordering against a unit that is not
		// installed is ignored by systemd, so images without cloud-init are unaffected.
		b.WriteString("Wants=network-online.target\nAfter=network-online.target cloud-final.service")
	}
	if after != "" {
		b.WriteString(" " + after)
	}
	b.WriteString("\n")
	if beforeLogin {
		b.WriteString("Before=systemd-user-sessions.service apt-daily.service apt-daily-upgrade.service\n")
	}
	// On an immutable image /etc is writable only once setup-overlay.service has
	// run, and the failure notice goes there. Ordering against an absent unit is ignored.
	b.WriteString("After=setup-overlay.service\n")
	if after != "" {
		// Requires= (not just After=) so a failed script stops the ones after it.
		fmt.Fprintf(b, "Requires=%s\n", after)
	}
	if isFirstBoot(script) {
		fmt.Fprintf(b, "ConditionPathExists=!%s/%s\n", StampDir, script.Name)
	}
	if script.StopOnFailure {
		// An Assert, not a Condition: a failed condition only skips the unit and
		// the units that require it would still run, while a failed assertion fails
		// the start job and keeps them blocked until the failure is cleared.
		fmt.Fprintf(b, "AssertPathExists=!%s/%s\n", FailedDir, script.Name)
	}
}

// writeServiceSection writes the [Service] body after Type= and RemainAfterExit=.
func writeServiceSection(b *strings.Builder, script config.ProvisioningScript, beforeLogin bool) {
	// systemd expands %-specifiers in ExecStart and Environment; escape
	// literal percent signs.
	for _, k := range slices.Sorted(maps.Keys(script.Env)) {
		fmt.Fprintf(b, "Environment=\"%s=%s\"\n", k, strings.ReplaceAll(script.Env[k], "%", "%%"))
	}
	exec := strings.Join(append([]string{script.Final}, script.Args...), " ")
	fmt.Fprintf(b, "ExecStart=%s\n", strings.ReplaceAll(exec, "%", "%%"))
	if isFirstBoot(script) {
		fmt.Fprintf(b, "ExecStartPost=/usr/bin/mkdir -p %s\n", StampDir)
		fmt.Fprintf(b, "ExecStartPost=/usr/bin/touch %s/%s\n", StampDir, script.Name)
	}
	fmt.Fprintf(b, "ExecStartPost=-%s ok %s\n", config.ProvisioningReportScript, script.Name)
	if script.RebootAfter {
		// After the stamp: the reboot job is queued only once success is recorded.
		b.WriteString("ExecStartPost=/usr/bin/systemctl reboot --no-block\n")
	}
	timeout := script.TimeoutSec
	if timeout == 0 && beforeLogin {
		timeout = config.DefaultBeforeLoginTimeoutSec
	}
	if timeout > 0 {
		fmt.Fprintf(b, "TimeoutStartSec=%d\n", timeout)
	}
	// Runs after a failed or timed-out start; the leading "-" keeps a problem
	// in the report from changing the unit's result.
	mode := "retry"
	if script.StopOnFailure {
		mode = "stop"
	}
	fmt.Fprintf(b, "ExecStopPost=-%s failed %s %%n %s\n", config.ProvisioningReportScript, script.Name, mode)
	b.WriteString("StandardOutput=journal+console\nStandardError=journal+console\n")
}

// renderProvisioningTrigger renders the unit that queues the script units
// without waiting for them: --no-block is what lets a script ordered after
// cloud-final.service start although multi-user.target is reached first, and
// the units run whether or not cloud-init does.
func renderProvisioningTrigger(units []string) string {
	return "# Generated by image-composer-tool\n[Unit]\nDescription=ICT provisioning trigger\n\n" +
		"[Service]\nType=oneshot\n" +
		"ExecStart=/usr/bin/systemctl start --no-block " + strings.Join(units, " ") + "\n\n" +
		"[Install]\nWantedBy=multi-user.target\n"
}

// enableUnit creates the multi-user.target.wants symlink, which is exactly
// what `systemctl enable` does for a WantedBy=multi-user.target unit.
func enableUnit(root *os.Root, unit string) error {
	link := wantsDir + "/" + unit
	if err := root.Remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("replacing /%s: %w", link, err)
	}
	if err := root.Symlink("/"+unitDir+"/"+unit, link); err != nil {
		return fmt.Errorf("enabling %s: %w", unit, err)
	}
	return nil
}
