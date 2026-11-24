//go:build !linux && !windows

package agent

// Screen brightness is collected on Linux and Windows only.
// platformBrightnessFn and externalBrightnessFn stay nil, so the field is absent.
