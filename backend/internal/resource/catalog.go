package resource

import (
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadCatalog parses a catalogs/<name>.yaml option list ([{value, label}, ...]).
// An empty catalog is an error — a select with no options is unusable.
func loadCatalog(path string) ([]Option, error) {
	data, err := readCapped(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read catalog: %w", err)
	}
	var opts []Option
	if err := yaml.Unmarshal(data, &opts); err != nil {
		return nil, fmt.Errorf("cannot parse catalog: %w", err)
	}
	if len(opts) == 0 {
		return nil, fmt.Errorf("catalog is empty")
	}
	for _, o := range opts {
		if o.Value == "" {
			return nil, fmt.Errorf("catalog has an option with an empty value")
		}
	}
	return opts, nil
}

// checkEgress is the v1 egress policy (F16): a declared URL must be http/https
// with a non-empty host. The private-IP/link-local/metadata denylist is
// intentionally deferred (V3 — arXiv's host is fixed + public); this is the seam
// where it drops in before any non-arXiv resource is enabled.
func checkEgress(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("url has no host")
	}
	return nil
}

// stripPlaceholders removes a trailing {{...}} path segment so a content URL
// template (…/html/{{paper.id}}) can be egress-checked on its static base.
func stripPlaceholders(rawURL string) string {
	if i := strings.Index(rawURL, "{{"); i != -1 {
		return strings.TrimSuffix(rawURL[:i], "/")
	}
	return rawURL
}
