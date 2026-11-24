package selfupdate

import (
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/henrygd/beszel/agent/health"
)

// FinishOptions is the post-restart health check. It is not given a command
// from the hub: the expected version is the one already verified on disk.
type FinishOptions struct {
	Expect   string
	Bin      string
	Attempts int
	Wait     func()
	Healthy  func() bool
	Version  func() string
	Restore  func() error
	Restart  func() error
}

// Finish keeps the new binary when it stays healthy, and restores the backup otherwise.
func Finish(opts FinishOptions) error {
	if opts.Attempts <= 0 {
		opts.Attempts = 1
	}
	if opts.Wait == nil {
		opts.Wait = func() { time.Sleep(2 * time.Second) }
	}
	healthy := false
	running := ""
	for i := 0; i < opts.Attempts; i++ {
		opts.Wait()
		if opts.Healthy != nil && opts.Healthy() {
			healthy = true
		}
		if opts.Version != nil {
			running = opts.Version()
		}
		if Outcome(healthy, running, opts.Expect) == "done" {
			return nil
		}
		healthy = false
	}
	if opts.Restore != nil {
		if err := opts.Restore(); err != nil {
			return err
		}
	}
	if opts.Restart != nil {
		if err := opts.Restart(); err != nil {
			return fmt.Errorf("rollback restart failed: %w", err)
		}
	}
	return errors.New("rolled back")
}

// FinishCLI is `beszel-agent update-finish`. Flags are local, not from the hub.
func FinishCLI(args []string) error {
	fs := flag.NewFlagSet("update-finish", flag.ContinueOnError)
	expect := fs.String("expect", "", "verified version")
	bin := fs.String("bin", "", "installed binary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *expect == "" || *bin == "" {
		return errors.New("expect and bin are required")
	}
	return Finish(FinishOptions{
		Expect:   *expect,
		Bin:      *bin,
		Attempts: 45,
		Healthy:  func() bool { return health.Check() == nil },
		Version: func() string {
			version, err := readBinaryVersion(*bin)
			if err != nil {
				return ""
			}
			return version
		},
		Restore: func() error { return restoreBackup(*bin) },
		Restart: restartSystemd,
	})
}
