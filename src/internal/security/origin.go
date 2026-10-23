package security

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

type origin struct {
	scheme string
	host   string
	port   string
}

func parseOrigin(value string, allowPath bool) (origin, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t\\") {
		return origin{}, ErrInvalidInput
	}
	u, err := url.Parse(value)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || u.Fragment != "" || u.RawFragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") || (!allowPath && (u.Path != "" || u.RawQuery != "" || u.ForceQuery)) {
		return origin{}, ErrInvalidInput
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.Contains(host, "%") || strings.HasSuffix(u.Host, ":") {
		return origin{}, ErrInvalidInput
	}
	if net.ParseIP(host) == nil {
		if len(host) > 253 {
			return origin{}, ErrInvalidInput
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return origin{}, ErrInvalidInput
			}
			for _, char := range label {
				if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
					return origin{}, ErrInvalidInput
				}
			}
		}
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	} else {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return origin{}, ErrInvalidInput
		}
		port = strconv.Itoa(number)
	}
	return origin{scheme: u.Scheme, host: host, port: port}, nil
}
