//go:build linux

package agent

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

func init() {
	platformBrightnessFn = func() *uint8 { return readSysfsBrightness(backlightRoot) }
	externalBrightnessFn = readDDCBrightness
	ddcutilAvailableFn = func() bool {
		_, err := exec.LookPath("ddcutil")
		return err == nil
	}
}

// readDDCBrightness runs `ddcutil getvcp 10`, the same command the fleet uses to
// read VCP 0x10. The percent is current/max, so a max other than 100 still works.
// Missing ddcutil, no display, and permission errors are distinct. None of them
// are reported as 0%.
func readDDCBrightness() (*uint8, error) {
	path, err := exec.LookPath("ddcutil")
	if err != nil {
		return nil, errDDCMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), ddcutilTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "getvcp", "10")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, errors.Join(errDDCFailed, ctx.Err())
	}
	text := output.String()
	if pct, ok := parseDDCBrightness(text); ok {
		return pct, nil
	}
	return nil, classifyDDCFailure(text, runErr)
}
