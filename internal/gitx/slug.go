package gitx

import "strings"

// SlugFromURL turns a remote URL into "owner/repo". It reads
// https://host/owner/repo(.git), ssh://git@host/owner/repo(.git) and
// git@host:owner/repo(.git). A URL without a host (a local path or file://)
// has no slug.
func SlugFromURL(raw string) (string, bool) {
	host, path, ok := splitRemote(strings.TrimSpace(raw))
	if !ok || host == "" {
		return "", false
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	owner, repo := parts[len(parts)-2], parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", false
	}
	return owner + "/" + repo, true
}

// splitRemote separates a remote URL into its host and path.
func splitRemote(raw string) (host, path string, ok bool) {
	if _, rest, found := strings.Cut(raw, "://"); found {
		hostPart, p, hasPath := strings.Cut(rest, "/")
		if !hasPath {
			return "", "", false
		}
		return stripUserPort(hostPart), p, true
	}
	hostPart, p, found := strings.Cut(raw, ":")
	if !found || strings.Contains(hostPart, "/") || strings.HasPrefix(p, "/") {
		return "", "", false // a local path, or an absolute path on a host
	}
	return stripUserPort(hostPart), p, true
}

func stripUserPort(h string) string {
	if at := strings.LastIndex(h, "@"); at >= 0 {
		h = h[at+1:]
	}
	if c := strings.LastIndex(h, ":"); c >= 0 {
		h = h[:c]
	}
	return h
}
