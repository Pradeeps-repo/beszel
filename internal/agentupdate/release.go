package agentupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	// DefaultRepo is the fork whose agent-latest release the fleet installs.
	DefaultRepo = "Pradeeps-repo/beszel"
	// DefaultTag is the rolling release tag.
	DefaultTag = "agent-latest"
)

// ReleaseBase is the download root for one tag. It never includes a file name.
func ReleaseBase(repo, tag string) string {
	if strings.TrimSpace(repo) == "" {
		repo = DefaultRepo
	}
	if strings.TrimSpace(tag) == "" {
		tag = DefaultTag
	}
	return "https://github.com/" + strings.Trim(repo, "/") + "/releases/download/" + strings.Trim(tag, "/")
}

// ValidateReleaseBase accepts only https://github.com/<repo>/releases/download/<tag>.
func ValidateReleaseBase(base, repo string) error {
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if repo == "" || strings.Contains(repo, "..") || strings.Count(repo, "/") != 1 {
		return errors.New("release repo must be owner/name")
	}
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil {
		return errors.New("release URL must be https://github.com/<repo>/releases/download/<tag>")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("release URL must not include a query or fragment")
	}
	prefix := "/" + repo + "/releases/download/"
	if !strings.HasPrefix(parsed.EscapedPath(), prefix) || strings.Contains(parsed.Path, "..") {
		return errors.New("release URL is not the configured repository")
	}
	tag := strings.TrimPrefix(parsed.Path, prefix)
	if tag == "" || strings.Contains(tag, "/") {
		return errors.New("release URL must name one tag")
	}
	return nil
}

// AssetName is the static binary published for this OS and architecture.
func AssetName(goos, goarch string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("unsupported os %s", goos)
	}
	switch goarch {
	case "amd64":
		return "beszel-agent_linux_amd64", nil
	case "arm64":
		return "beszel-agent_linux_arm64", nil
	case "arm":
		return "beszel-agent_linux_armv7", nil
	default:
		return "", fmt.Errorf("unsupported arch %s", goarch)
	}
}

// ParseSHA256 reads a sha256sum line or a bare hash.
func ParseSHA256(text string) (string, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", errors.New("checksum file is empty")
	}
	sum := strings.ToLower(fields[0])
	if len(sum) != sha256.Size*2 {
		return "", errors.New("checksum is not sha256")
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", errors.New("checksum is not sha256")
	}
	return sum, nil
}

// VersionFile is beszel-agent_version from the release.
type VersionFile struct {
	Version string
	Commit  string
}

// ParseVersionFile reads version= and commit= lines.
func ParseVersionFile(text string) (VersionFile, error) {
	var file VersionFile
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			if file.Version == "" {
				file.Version = line
			}
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "version":
			file.Version = strings.TrimSpace(value)
		case "commit":
			file.Commit = strings.TrimSpace(value)
		}
	}
	if file.Version == "" {
		return VersionFile{}, errors.New("version file has no version")
	}
	return file, nil
}
