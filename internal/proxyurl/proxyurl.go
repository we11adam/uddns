package proxyurl

import (
	"fmt"
	"net/url"
	"strings"
)

// Parse validates an HTTP proxy URL without including the input in errors,
// because it may contain credentials in userinfo.
func Parse(rawURL string) (*url.URL, error) {
	proxyURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL syntax")
	}

	proxyURL.Scheme = strings.ToLower(proxyURL.Scheme)
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return nil, fmt.Errorf("proxy URL scheme must be http or https")
	}
	if !proxyURL.IsAbs() || proxyURL.Opaque != "" || proxyURL.Hostname() == "" {
		return nil, fmt.Errorf("proxy URL must be absolute and include a host")
	}
	if proxyURL.RawQuery != "" || proxyURL.ForceQuery || proxyURL.Fragment != "" || strings.Contains(rawURL, "#") {
		return nil, fmt.Errorf("proxy URL must not include a query or fragment")
	}
	if path := proxyURL.EscapedPath(); path != "" && path != "/" {
		return nil, fmt.Errorf("proxy URL path must be empty or /")
	}

	return proxyURL, nil
}
