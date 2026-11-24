package agentupdate

import (
	"strconv"
	"strings"
)

type parsedVersion struct {
	major int
	minor int
	patch int
	bw    int
	hasBW bool
}

// Compare orders fleet build stamps. 0.21.2-bw.<run>+<commit> is newer than
// plain 0.21.2 of the same core, and a higher run number is newer. The +commit
// is ignored. An unreadable value is older than a readable one.
func Compare(a, b string) int {
	left, leftOK := parseVersion(a)
	right, rightOK := parseVersion(b)
	if !leftOK && !rightOK {
		return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	if !leftOK {
		return -1
	}
	if !rightOK {
		return 1
	}
	if left.major != right.major {
		return left.major - right.major
	}
	if left.minor != right.minor {
		return left.minor - right.minor
	}
	if left.patch != right.patch {
		return left.patch - right.patch
	}
	if left.hasBW != right.hasBW {
		if left.hasBW {
			return 1
		}
		return -1
	}
	return left.bw - right.bw
}

func parseVersion(value string) (parsedVersion, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	if i := strings.IndexByte(value, '+'); i >= 0 {
		value = value[:i]
	}
	if value == "" {
		return parsedVersion{}, false
	}
	core, pre, _ := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) < 3 {
		return parsedVersion{}, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	patch, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return parsedVersion{}, false
	}
	parsed := parsedVersion{major: major, minor: minor, patch: patch}
	if strings.HasPrefix(pre, "bw.") {
		n, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(pre, "bw."), ".", 2)[0])
		if err != nil {
			return parsedVersion{}, false
		}
		parsed.bw = n
		parsed.hasBW = true
	}
	return parsed, true
}
