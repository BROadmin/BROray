package routepolicy

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"unicode/utf8"
)

func domainValue(input string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(input))
	prefix := "domain:"
	if strings.HasPrefix(value, "domain:") {
		value = strings.TrimPrefix(value, "domain:")
	} else if strings.HasPrefix(value, "full:") {
		prefix = "full:"
		value = strings.TrimPrefix(value, "full:")
	}
	if strings.HasPrefix(value, "*.") && prefix == "domain:" {
		value = strings.TrimPrefix(value, "*.")
	}
	value = strings.TrimSuffix(value, ".")
	if len(value) == 0 || len(value) > 253 || net.ParseIP(value) != nil {
		return "", fmt.Errorf("invalid domain %q", input)
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid domain label in %q", input)
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", fmt.Errorf("use a DNS name in ASCII or punycode: %q", input)
			}
		}
	}
	return prefix + value, nil
}

func ParseDomainList(text string) ([]string, error) {
	if len(text) > 2<<20 || !utf8.ValidString(text) {
		return nil, fmt.Errorf("domain list exceeds limit or is not UTF-8")
	}
	text = strings.TrimPrefix(text, "\ufeff")
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		value, err := domainValue(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		seen[value] = true
		if len(seen) > 50000 {
			return nil, fmt.Errorf("too many domains")
		}
	}
	return sortedKeys(seen), nil
}

func sortedKeys(seen map[string]bool) []string {
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
