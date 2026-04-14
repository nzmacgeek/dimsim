package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// clawServicesDir is where claw reads service unit files.
const clawServicesDir = "/etc/claw/services.d"

// firstbootBaseDir is where staged lifecycle scripts are kept on the target.
const firstbootBaseDir = "/var/lib/dimsim/firstboot"

// truncateString returns the first n bytes of s, or s if len(s) <= n.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// hasValidShebang reports whether s begins with a recognised interpreter
// declaration (#!/bin/bash or #!/bin/sh) optionally followed by a newline
// (LF or CRLF) or end of string.
func hasValidShebang(s string) bool {
	for _, interp := range []string{"#!/bin/bash", "#!/bin/sh"} {
		if !strings.HasPrefix(s, interp) {
			continue
		}
		rest := s[len(interp):]
		if rest == "" || strings.HasPrefix(rest, "\r\n") || strings.HasPrefix(rest, "\n") {
			return true
		}
	}
	return false
}

// validateShebang returns an error if s does not start with a valid shebang.
// label distinguishes the kind of script (e.g. "lifecycle script", "firstboot wrapper").
func validateShebang(label, pkgName, scriptName, s string) error {
	if !hasValidShebang(s) {
		return fmt.Errorf("%s validation failed for %s/%s: script does not start with a valid shebang (got first 32 bytes: %q)",
			label, pkgName, scriptName, truncateString(s, 32))
	}
	return nil
}

// ValidateBlueyOSRoot checks that rootDir looks like a BlueyOS root filesystem.
// It requires /etc/claw/ (the claw init system config directory) and /bin/bash.
func ValidateBlueyOSRoot(rootDir string) error {
	checks := []struct {
		path string
		hint string
	}{
		{"/etc/claw", "missing /etc/claw/ (claw init system config directory)"},
		{"/bin/bash", "missing /bin/bash (required by BlueyOS — no /bin/sh)"},
	}
	for _, c := range checks {
		full := filepath.Join(rootDir, strings.TrimLeft(c.path, string(filepath.Separator)))
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return fmt.Errorf(
				"target root %q does not appear to be a BlueyOS system:\n  %s",
				rootDir, c.hint,
			)
		}
	}
	return nil
}

// validateBlueyOSRootNoBash checks that rootDir has /etc/claw/ but does NOT
// require /bin/bash. Used when all packages being installed are marked as core,
// allowing foundational packages (such as bash itself) to be installed into a
// fresh sysroot before bash is present.
func validateBlueyOSRootNoBash(rootDir string) error {
	full := filepath.Join(rootDir, strings.TrimLeft("/etc/claw", string(filepath.Separator)))
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return fmt.Errorf(
			"target root %q does not appear to be a BlueyOS system:\n  missing /etc/claw/ (claw init system config directory)",
			rootDir,
		)
	}
	return nil
}

// stageFirstBootScript stages a lifecycle script (preinst or postinst) as a
// claw oneshot service that runs exactly once on the target's first boot.
//
// It writes three files into the target rootfs:
//
//  1. The raw script:
//     <rootDir>/var/lib/dimsim/firstboot/<pkg>/<scriptName>.sh
//
//  2. A self-removing wrapper (so the service doesn't run on subsequent boots):
//     <rootDir>/var/lib/dimsim/firstboot/<pkg>/run-<scriptName>
//
//  3. A claw service unit:
//     <rootDir>/etc/claw/services.d/dimsim-<scriptName>-<pkg>.yml
//
// afterUnits lists additional claw unit names that must complete before this
// service starts (e.g. the preinst of the same package before its postinst).
func (ins *Installer) stageFirstBootScript(pkgName, scriptName, script string, afterUnits []string) error {
	// --- 1. Raw script -------------------------------------------------------
	fbDir := ins.rootPath(filepath.Join(firstbootBaseDir, pkgName))
	if err := os.MkdirAll(fbDir, 0755); err != nil {
		return fmt.Errorf("create firstboot dir for %s: %w", pkgName, err)
	}

	// Paths as seen by the TARGET system (no rootDir prefix)
	scriptTargetPath := filepath.Join(firstbootBaseDir, pkgName, scriptName+".sh")
	wrapperTargetPath := filepath.Join(firstbootBaseDir, pkgName, "run-"+scriptName)
	svcName := fmt.Sprintf("dimsim-%s-%s", scriptName, pkgName)
	svcTargetPath := filepath.Join(clawServicesDir, svcName+".yml")

	// Validate that the raw lifecycle script starts with a valid shebang.
	// This ensures we don't write corrupted or malformed scripts to the target.
	if err := validateShebang("lifecycle script", pkgName, scriptName, script); err != nil {
		return err
	}

	// Write the raw lifecycle script to the target rootfs
	if err := os.WriteFile(ins.rootPath(scriptTargetPath), []byte(script), 0755); err != nil {
		return fmt.Errorf("write firstboot script %s/%s: %w", pkgName, scriptName, err)
	}

	// --- 2. Wrapper script ---------------------------------------------------
	// The wrapper runs the lifecycle script then deletes the service file so
	// claw won't activate it on subsequent boots.
	wrapper := fmt.Sprintf(`#!/bin/bash
# dimsim firstboot wrapper — %s for package %s
# This script is generated automatically. Do not edit.
# It runs once on the target's first boot and then removes itself.
set -e

# Run the lifecycle script
/bin/bash %s
EXIT_CODE=$?

# Self-remove: prevent this service from running again on subsequent boots
rm -f %s

exit $EXIT_CODE
`, scriptName, pkgName, scriptTargetPath, svcTargetPath)

	// Validate that the wrapper starts with a valid shebang before writing to disk.
	// This catches corrupted buffers or template errors early.
	if err := validateShebang("firstboot wrapper", pkgName, scriptName, wrapper); err != nil {
		return err
	}

	if err := os.WriteFile(ins.rootPath(wrapperTargetPath), []byte(wrapper), 0755); err != nil {
		return fmt.Errorf("write firstboot wrapper %s/%s: %w", pkgName, scriptName, err)
	}

	// --- 3. Claw service unit ------------------------------------------------
	// Build the `after:` list. Always wait for the root filesystem to be ready;
	// append any caller-specified units (e.g. the preinst service).
	after := []string{"claw-rootfs.target"}
	after = append(after, afterUnits...)

	svcContent := fmt.Sprintf(`# Auto-generated by dimsim offline install — do not edit manually.
name: %s
description: %s hook for %s (dimsim firstboot, runs once)
type: oneshot
exec_start: /bin/bash %s
after: %s
before: claw-multiuser.target
restart: no
`, svcName, scriptName, pkgName, wrapperTargetPath, strings.Join(after, " "))

	svcDest := ins.rootPath(svcTargetPath)
	if err := os.MkdirAll(filepath.Dir(svcDest), 0755); err != nil {
		return fmt.Errorf("create claw services dir: %w", err)
	}
	if err := os.WriteFile(svcDest, []byte(svcContent), 0644); err != nil {
		return fmt.Errorf("write claw service file for %s/%s: %w", pkgName, scriptName, err)
	}

	return nil
}
