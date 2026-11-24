//go:build linux

package agent

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

func init() {
	platformBrightnessFn = func() *uint8 { return readSysfsBrightness(backlightRoot) }
	externalBrightnessFn = readDDCBrightness
}

// readDDCBrightness asks ddcutil for VCP code 10 (brightness) when sysfs has no panel.
// Missing ddcutil is absent, not an error. The command is killed if it stalls.
func readDDCBrightness() (*uint8, error) {
	path, err := exec.LookPath("ddcutil")
	if err != nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "getvcp", "10", "--brief")
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("ddcutil timed out")
	}
	if err != nil {
		return nil, err
	}
	pct, ok := parseDDCBrightness(string(out))
	if !ok {
		return nil, nil
	}
	return pct, nil
}
