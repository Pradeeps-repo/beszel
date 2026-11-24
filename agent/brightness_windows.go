//go:build windows

package agent

import (
	"log/slog"

	"github.com/yusufpapurcu/wmi"
)

func init() {
	platformBrightnessFn = windowsBrightness
}

func windowsBrightness() *uint8 {
	var monitors []wmiMonitorBrightness
	err := wmi.QueryNamespace(
		"SELECT InstanceName, CurrentBrightness, Active FROM WmiMonitorBrightness",
		&monitors,
		`root\wmi`,
	)
	if err != nil {
		slog.Debug("Error reading display brightness", "err", err)
		return nil
	}
	return brightnessFromWMI(monitors)
}
