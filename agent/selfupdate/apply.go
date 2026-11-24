package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/henrygd/beszel/internal/agentupdate"
)

const maxBinaryBytes = 80 << 20

// Config is everything Apply needs. The hub cannot supply a URL or a command.
type Config struct {
	Version     string
	ReleaseBase string
	Repo        string
	GOOS        string
	GOARCH      string
	BinPath     string
	Client      *http.Client
	VersionOf   func(path string) (string, error)
	IsRoot      bool
	InstallHelp func() error
	ArmWatcher  func(expect string) error
	Restart     func() error
}

// Apply downloads the configured release, checks sha256 and the stamped version,
// keeps a backup, swaps the binary, and restarts. A failed restart restores the backup.
func Apply(ctx context.Context, cfg Config) (warn error, err error) {
	if strings.TrimSpace(cfg.Version) == "" {
		return nil, errors.New("target version is required")
	}
	if err := agentupdate.ValidateReleaseBase(cfg.ReleaseBase, cfg.Repo); err != nil {
		return nil, err
	}
	asset, err := agentupdate.AssetName(cfg.GOOS, cfg.GOARCH)
	if err != nil {
		return nil, err
	}
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	dir, err := os.MkdirTemp("", "beszel-update-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	staged := filepath.Join(dir, asset)
	base := strings.TrimRight(cfg.ReleaseBase, "/")
	if err := download(ctx, cfg.Client, base+"/"+asset, staged, maxBinaryBytes); err != nil {
		return nil, err
	}
	sumBody, err := downloadText(ctx, cfg.Client, base+"/"+asset+".sha256")
	if err != nil {
		return nil, err
	}
	want, err := agentupdate.ParseSHA256(sumBody)
	if err != nil {
		return nil, err
	}
	if err := verifySHA256(staged, want); err != nil {
		return nil, err
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		return nil, err
	}
	versionOf := cfg.VersionOf
	if versionOf == nil {
		versionOf = readBinaryVersion
	}
	got, err := versionOf(staged)
	if err != nil {
		return nil, err
	}
	if agentupdate.Compare(got, cfg.Version) != 0 {
		return nil, fmt.Errorf("downloaded %s, hub asked for %s", got, cfg.Version)
	}
	if cfg.IsRoot {
		if cfg.InstallHelp != nil {
			if err := cfg.InstallHelp(); err != nil {
				return nil, err
			}
		}
	}
	if err := installBinary(cfg.BinPath, staged); err != nil {
		return nil, err
	}
	if cfg.ArmWatcher != nil {
		if err := cfg.ArmWatcher(got); err != nil {
			_ = restoreBackup(cfg.BinPath)
			return nil, err
		}
	}
	if cfg.Restart != nil {
		if err := cfg.Restart(); err != nil {
			_ = restoreBackup(cfg.BinPath)
			return nil, fmt.Errorf("restart failed, backup restored: %w", err)
		}
	}
	return nil, nil
}

func download(ctx context.Context, client *http.Client, rawURL, dest string, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned %s", rawURL, res.Status)
	}
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	n, err := io.Copy(file, io.LimitReader(res.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("download exceeded %d bytes", limit)
	}
	return nil
}

func downloadText(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s returned %s", rawURL, res.Status)
	}
	return string(body), nil
}

func verifySHA256(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}
	got := hex.EncodeToString(sum.Sum(nil))
	if !strings.EqualFold(got, want) {
		return errors.New("sha256 mismatch")
	}
	return nil
}

func installBinary(dest, staged string) error {
	if dest == "" {
		return errors.New("binary path is required")
	}
	if _, err := os.Stat(dest); err == nil {
		if err := copyFile(dest, dest+".bak"); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
	}
	next := dest + ".new"
	if err := copyFile(staged, next); err != nil {
		return err
	}
	if err := os.Chmod(next, 0o755); err != nil {
		return err
	}
	return os.Rename(next, dest)
}

func restoreBackup(dest string) error {
	backup := dest + ".bak"
	if _, err := os.Stat(backup); err != nil {
		return err
	}
	return copyFile(backup, dest)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// Outcome decides whether the new process is healthy enough to keep.
func Outcome(healthy bool, running, expect string) string {
	if healthy && agentupdate.Compare(running, expect) == 0 {
		return "done"
	}
	return "rolled_back"
}
