package mcp

import (
	"context"
	"slices"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/henrygd/beszel/internal/entities/system"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// packageDetail is one pending package on a system.
// current is the installed version when the agent knows it.
// available is the candidate version.
type packageDetail struct {
	Name      string `json:"name"`
	Current   string `json:"current,omitempty"`
	Available string `json:"available,omitempty"`
	Security  bool   `json:"security,omitempty"`
}

type pendingPackageRow struct {
	Name        string `json:"name"`
	Available   string `json:"available,omitempty"`
	Security    bool   `json:"security,omitempty"`
	SystemCount int    `json:"system_count"`
}

type pendingPackageList struct {
	Count     int                 `json:"count"`
	Total     int                 `json:"total"`
	Offset    int                 `json:"offset,omitzero"`
	Truncated bool                `json:"truncated,omitzero"`
	Packages  []pendingPackageRow `json:"packages"`
}

type listPendingPackagesParams struct {
	Package      string `json:"package,omitempty" jsonschema:"only packages whose name contains this text"`
	SecurityOnly bool   `json:"security_only,omitempty" jsonschema:"only packages flagged as security updates"`
	Os           string `json:"os,omitempty" jsonschema:"only systems whose OS text contains this, for example Ubuntu"`
	Status       string `json:"status,omitempty" jsonschema:"only systems with this status: up, down or paused"`
	Limit        int    `json:"limit,omitempty" jsonschema:"page size, default 200, maximum 200"`
	Offset       int    `json:"offset,omitempty" jsonschema:"number of matching packages to skip"`
}

func recordPackageUpdates(record *core.Record) system.PackageUpdates {
	var updates system.PackageUpdates
	if record == nil || record.Get("package_updates") == nil {
		return updates
	}
	if err := record.UnmarshalJSONField("package_updates", &updates); err != nil {
		return system.PackageUpdates{}
	}
	return updates
}

func packageDetails(packages []system.PackageUpdate) []packageDetail {
	details := make([]packageDetail, 0, len(packages))
	for _, pkg := range packages {
		name := strings.TrimSpace(pkg.Name)
		if name == "" {
			continue
		}
		details = append(details, packageDetail{
			Name:      name,
			Current:   pkg.Current,
			Available: pkg.Available,
			Security:  pkg.Security,
		})
	}
	return details
}

func systemHasPackage(record *core.Record, name string) bool {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return true
	}
	for _, pkg := range recordPackageUpdates(record).Packages {
		if strings.Contains(strings.ToLower(pkg.Name), needle) {
			return true
		}
	}
	return false
}

func systemMatchesPackageFilters(record *core.Record, params listPendingPackagesParams) bool {
	if status := strings.ToLower(strings.TrimSpace(params.Status)); status != "" && !strings.EqualFold(record.GetString("status"), status) {
		return false
	}
	if osName := strings.TrimSpace(params.Os); osName != "" {
		var info system.Info
		_ = record.UnmarshalJSONField("info", &info)
		if !strings.Contains(strings.ToLower(info.OsRelease), strings.ToLower(osName)) {
			return false
		}
	}
	return true
}

// listPendingPackages groups the stored pending packages of visible systems.
func (h *mcphub) listPendingPackages(ctx context.Context, _ *mcpsdk.CallToolRequest, params listPendingPackagesParams) (*mcpsdk.CallToolResult, pendingPackageList, error) {
	auth, err := h.authRecord(ctx)
	if err != nil {
		return nil, pendingPackageList{}, err
	}
	records, err := h.listAllVisibleSystems(auth)
	if err != nil {
		return nil, pendingPackageList{}, err
	}
	type group struct {
		pendingPackageRow
		seen map[string]struct{}
	}
	groups := map[string]*group{}
	order := make([]string, 0)
	needle := strings.ToLower(strings.TrimSpace(params.Package))
	for _, record := range records {
		if !systemMatchesPackageFilters(record, params) {
			continue
		}
		for _, pkg := range packageDetails(recordPackageUpdates(record).Packages) {
			if params.SecurityOnly && !pkg.Security {
				continue
			}
			if needle != "" && !strings.Contains(strings.ToLower(pkg.Name), needle) {
				continue
			}
			key := pkg.Name + "\x00" + pkg.Available + "\x00"
			if pkg.Security {
				key += "1"
			}
			row := groups[key]
			if row == nil {
				row = &group{pendingPackageRow: pendingPackageRow{
					Name:      pkg.Name,
					Available: pkg.Available,
					Security:  pkg.Security,
				}, seen: map[string]struct{}{}}
				groups[key] = row
				order = append(order, key)
			}
			row.seen[record.Id] = struct{}{}
		}
	}
	rows := make([]pendingPackageRow, 0, len(order))
	for _, key := range order {
		row := groups[key]
		row.SystemCount = len(row.seen)
		rows = append(rows, row.pendingPackageRow)
	}
	slices.SortFunc(rows, func(a, b pendingPackageRow) int {
		if a.SystemCount != b.SystemCount {
			return b.SystemCount - a.SystemCount
		}
		if cmp := strings.Compare(a.Name, b.Name); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Available, b.Available)
	})
	limit := params.Limit
	if limit <= 0 || limit > maxListRecords {
		limit = maxListRecords
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(rows) {
		offset = len(rows)
	}
	end := offset + limit
	truncated := end < len(rows)
	if end > len(rows) {
		end = len(rows)
	}
	page := rows[offset:end]
	if page == nil {
		page = []pendingPackageRow{}
	}
	return nil, pendingPackageList{
		Count:     len(page),
		Total:     len(rows),
		Offset:    offset,
		Truncated: truncated,
		Packages:  page,
	}, nil
}
