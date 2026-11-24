package agentupdate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const versionFileName = "beszel-agent_version"

// FetchVersion downloads the version file published next to the agent binaries.
func FetchVersion(ctx context.Context, client *http.Client, repo, tag string) (VersionFile, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(repo) == "" {
		repo = DefaultRepo
	}
	if strings.TrimSpace(tag) == "" {
		tag = DefaultTag
	}
	base := ReleaseBase(repo, tag)
	if err := ValidateReleaseBase(base, repo); err != nil {
		return VersionFile{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/"+versionFileName, nil)
	if err != nil {
		return VersionFile{}, err
	}
	req.Header.Set("Accept", "text/plain")
	res, err := client.Do(req)
	if err != nil {
		return VersionFile{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return VersionFile{}, err
	}
	if res.StatusCode != http.StatusOK {
		return VersionFile{}, fmt.Errorf("version file returned %s", res.Status)
	}
	return ParseVersionFile(string(body))
}
