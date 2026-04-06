package evm

import "strings"

func DeriveHTTPURL(wsURL string) string {
	if strings.HasPrefix(wsURL, "https://") || strings.HasPrefix(wsURL, "http://") {
		return wsURL
	}
	if strings.HasPrefix(wsURL, "wss://") {
		return "https://" + strings.TrimPrefix(wsURL, "wss://")
	}
	if strings.HasPrefix(wsURL, "ws://") {
		return "http://" + strings.TrimPrefix(wsURL, "ws://")
	}
	return wsURL
}
