package subscriptions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var proxyURISchemes = map[string]bool{
	"ss":        true,
	"ssr":       true,
	"vmess":     true,
	"vless":     true,
	"trojan":    true,
	"hysteria":  true,
	"hysteria2": true,
	"hy2":       true,
	"tuic":      true,
	"anytls":    true,
	"socks":     true,
	"socks5":    true,
	"socks5h":   true,
}

func parseProxyURIList(sourceID string, data []byte) (subscriptionDoc, error) {
	lines := strings.Split(string(data), "\n")
	seen := map[string]bool{}
	var proxies []any
	var accepted int
	for idx, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		scheme, rest, ok := strings.Cut(line, "://")
		if !ok || !proxyURISchemes[strings.ToLower(scheme)] || strings.TrimSpace(rest) == "" {
			return subscriptionDoc{}, fmt.Errorf("source %q proxy URI line %d is not an MVP proxy URI", sourceID, idx+1)
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		proxy, err := parseProxyURI(line, accepted+1)
		if err != nil {
			return subscriptionDoc{}, fmt.Errorf("source %q proxy URI line %d: %w", sourceID, idx+1, err)
		}
		proxies = append(proxies, proxy)
		accepted++
	}
	if len(proxies) == 0 {
		return subscriptionDoc{}, fmt.Errorf("source %q has no MVP proxy URI lines", sourceID)
	}
	return subscriptionDoc{
		Data:   map[string]any{"proxies": proxies},
		Raw:    append([]byte(nil), data...),
		Format: subscriptionFormatProxyURILines,
	}, nil
}

func parseProxyURI(raw string, index int) (map[string]any, error) {
	scheme, _, _ := strings.Cut(raw, "://")
	scheme = strings.ToLower(scheme)
	switch scheme {
	case "ss":
		return parseSSURI(raw, index)
	case "ssr":
		return parseSSRURI(raw, index)
	case "vmess":
		return parseVMessURI(raw, index)
	case "vless":
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		proxy, err := parseVShareURI(u, "vless", index)
		if err != nil {
			return nil, err
		}
		query := u.Query()
		if flow := query.Get("flow"); flow != "" {
			proxy["flow"] = strings.ToLower(flow)
		}
		if encryption := query.Get("encryption"); encryption != "" {
			proxy["encryption"] = encryption
		}
		return proxy, nil
	case "trojan":
		return parseTrojanURI(raw, index)
	case "hysteria":
		return parseHysteriaURI(raw, index)
	case "hysteria2", "hy2":
		return parseHysteria2URI(raw, index)
	case "tuic":
		return parseTUICURI(raw, index)
	case "anytls":
		return parseAnyTLSURI(raw, index)
	case "socks", "socks5", "socks5h":
		return parseSocksURI(raw, index)
	default:
		return nil, fmt.Errorf("unsupported proxy URI scheme %q", scheme)
	}
}

func parseSSURI(raw string, index int) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Port() == "" {
		decoded, err := decodeBase64String(u.Host)
		if err != nil {
			return nil, fmt.Errorf("invalid ss host payload")
		}
		u, err = url.Parse("ss://" + decoded)
		if err != nil {
			return nil, err
		}
	}
	cipher := u.User.Username()
	password, ok := u.User.Password()
	if !ok {
		decoded, err := decodeBase64String(cipher)
		if err != nil {
			return nil, fmt.Errorf("invalid ss userinfo")
		}
		cipher, password, ok = strings.Cut(decoded, ":")
		if !ok {
			return nil, fmt.Errorf("invalid ss userinfo")
		}
	}
	proxy, err := basicHostPortProxy(u, "ss", index)
	if err != nil {
		return nil, err
	}
	proxy["cipher"] = cipher
	proxy["password"] = password
	proxy["udp"] = true
	query := u.Query()
	if query.Get("udp-over-tcp") == "true" || query.Get("uot") == "1" {
		proxy["udp-over-tcp"] = true
	}
	return proxy, nil
}

func parseSSRURI(raw string, index int) (map[string]any, error) {
	_, body, _ := strings.Cut(raw, "://")
	decoded, err := decodeBase64String(body)
	if err != nil {
		return nil, fmt.Errorf("invalid ssr payload")
	}
	before, after, ok := strings.Cut(decoded, "/?")
	if !ok {
		return nil, fmt.Errorf("invalid ssr payload")
	}
	parts := strings.Split(before, ":")
	if len(parts) != 6 {
		return nil, fmt.Errorf("invalid ssr server fields")
	}
	query, err := url.ParseQuery(urlSafe(after))
	if err != nil {
		return nil, err
	}
	name := decodeURLSafe(query.Get("remarks"))
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("ssr-%s:%s", parts[0], parts[1])
	}
	proxy := map[string]any{
		"name":     name,
		"type":     "ssr",
		"server":   parts[0],
		"port":     parts[1],
		"protocol": parts[2],
		"cipher":   parts[3],
		"obfs":     parts[4],
		"password": decodeURLSafe(parts[5]),
		"udp":      true,
	}
	if value := decodeURLSafe(query.Get("obfsparam")); value != "" {
		proxy["obfs-param"] = value
	}
	if value := decodeURLSafe(query.Get("protoparam")); value != "" {
		proxy["protocol-param"] = value
	}
	return proxy, nil
}

func parseVMessURI(raw string, index int) (map[string]any, error) {
	_, body, _ := strings.Cut(raw, "://")
	decoded, err := decodeBase64String(body)
	if err == nil {
		var values map[string]any
		if json.Unmarshal([]byte(decoded), &values) == nil {
			return parseVMessJSON(values, index)
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	proxy, err := parseVShareURI(u, "vmess", index)
	if err != nil {
		return nil, err
	}
	proxy["alterId"] = 0
	proxy["cipher"] = "auto"
	if encryption := u.Query().Get("encryption"); encryption != "" {
		proxy["cipher"] = encryption
	}
	return proxy, nil
}

func parseVMessJSON(values map[string]any, index int) (map[string]any, error) {
	name := stringFromAny(values["ps"])
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("vmess-%d", index)
	}
	proxy := map[string]any{
		"name":             name,
		"type":             "vmess",
		"server":           values["add"],
		"port":             values["port"],
		"uuid":             values["id"],
		"alterId":          0,
		"udp":              true,
		"xudp":             true,
		"tls":              false,
		"skip-cert-verify": false,
		"cipher":           "auto",
	}
	if alterID, ok := values["aid"]; ok {
		proxy["alterId"] = alterID
	}
	if cipher := stringFromAny(values["scy"]); cipher != "" {
		proxy["cipher"] = cipher
	}
	if sni := stringFromAny(values["sni"]); sni != "" {
		proxy["servername"] = sni
	}
	network := strings.ToLower(stringFromAny(values["net"]))
	if network != "" {
		if stringFromAny(values["type"]) == "http" {
			network = "http"
		} else if network == "http" {
			network = "h2"
		}
		proxy["network"] = network
	}
	tls := strings.ToLower(stringFromAny(values["tls"]))
	if strings.HasSuffix(tls, "tls") {
		proxy["tls"] = true
		if alpn := stringFromAny(values["alpn"]); alpn != "" {
			proxy["alpn"] = strings.Split(alpn, ",")
		}
	}
	applyTransportOptions(proxy, network, stringFromAny(values["host"]), stringFromAny(values["path"]), "")
	return proxy, nil
}

func parseVShareURI(u *url.URL, proxyType string, index int) (map[string]any, error) {
	proxy, err := basicHostPortProxy(u, proxyType, index)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	proxy["uuid"] = u.User.Username()
	proxy["udp"] = true
	tls := strings.ToLower(query.Get("security"))
	if strings.HasSuffix(tls, "tls") || tls == "reality" {
		proxy["tls"] = true
		if fp := query.Get("fp"); fp != "" {
			proxy["client-fingerprint"] = fp
		} else {
			proxy["client-fingerprint"] = "chrome"
		}
		if alpn := query.Get("alpn"); alpn != "" {
			proxy["alpn"] = strings.Split(alpn, ",")
		}
		if pcs := query.Get("pcs"); pcs != "" {
			proxy["fingerprint"] = pcs
		}
	}
	if sni := query.Get("sni"); sni != "" {
		proxy["servername"] = sni
	}
	if publicKey := query.Get("pbk"); publicKey != "" {
		proxy["reality-opts"] = map[string]any{"public-key": publicKey, "short-id": query.Get("sid")}
	}
	switch query.Get("packetEncoding") {
	case "none":
	case "packet":
		proxy["packet-addr"] = true
	default:
		proxy["xudp"] = true
	}
	network := strings.ToLower(query.Get("type"))
	if network == "" {
		network = "tcp"
	}
	if network == "tcp" && strings.ToLower(query.Get("headerType")) == "http" {
		network = "http"
	} else if network == "http" {
		network = "h2"
	}
	proxy["network"] = network
	applyTransportOptions(proxy, network, query.Get("host"), query.Get("path"), query.Get("serviceName"))
	return proxy, nil
}

func parseTrojanURI(raw string, index int) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	proxy, err := basicHostPortProxy(u, "trojan", index)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	proxy["password"] = u.User.Username()
	proxy["udp"] = true
	if value, _ := strconv.ParseBool(query.Get("allowInsecure")); value {
		proxy["skip-cert-verify"] = true
	}
	if sni := query.Get("sni"); sni != "" {
		proxy["sni"] = sni
	}
	if alpn := query.Get("alpn"); alpn != "" {
		proxy["alpn"] = strings.Split(alpn, ",")
	}
	network := strings.ToLower(query.Get("type"))
	if network != "" {
		proxy["network"] = network
		applyTransportOptions(proxy, network, query.Get("host"), query.Get("path"), query.Get("serviceName"))
	}
	if fp := query.Get("fp"); fp != "" {
		proxy["client-fingerprint"] = fp
	} else {
		proxy["client-fingerprint"] = "chrome"
	}
	if pcs := query.Get("pcs"); pcs != "" {
		proxy["fingerprint"] = pcs
	}
	return proxy, nil
}

func parseHysteriaURI(raw string, index int) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	proxy, err := basicHostPortProxy(u, "hysteria", index)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	proxy["sni"] = query.Get("peer")
	proxy["obfs"] = query.Get("obfs")
	if alpn := query.Get("alpn"); alpn != "" {
		proxy["alpn"] = strings.Split(alpn, ",")
	}
	proxy["auth_str"] = query.Get("auth")
	proxy["protocol"] = query.Get("protocol")
	up, down := query.Get("up"), query.Get("down")
	if up == "" {
		up = query.Get("upmbps")
	}
	if down == "" {
		down = query.Get("downmbps")
	}
	proxy["up"] = up
	proxy["down"] = down
	proxy["skip-cert-verify"], _ = strconv.ParseBool(query.Get("insecure"))
	return proxy, nil
}

func parseHysteria2URI(raw string, index int) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	proxy, err := basicHostPortProxyWithDefaultPort(u, "hysteria2", "443", index)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	proxy["password"] = u.User.String()
	proxy["obfs"] = query.Get("obfs")
	proxy["obfs-password"] = query.Get("obfs-password")
	proxy["sni"] = query.Get("sni")
	proxy["skip-cert-verify"], _ = strconv.ParseBool(query.Get("insecure"))
	if alpn := query.Get("alpn"); alpn != "" {
		proxy["alpn"] = strings.Split(alpn, ",")
	}
	proxy["fingerprint"] = query.Get("pinSHA256")
	proxy["down"] = query.Get("down")
	proxy["up"] = query.Get("up")
	return proxy, nil
}

func parseTUICURI(raw string, index int) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	proxy, err := basicHostPortProxy(u, "tuic", index)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	proxy["udp"] = true
	if password, ok := u.User.Password(); ok {
		proxy["uuid"] = u.User.Username()
		proxy["password"] = password
	} else {
		proxy["token"] = u.User.Username()
	}
	if cc := query.Get("congestion_control"); cc != "" {
		proxy["congestion-controller"] = cc
	}
	if alpn := query.Get("alpn"); alpn != "" {
		proxy["alpn"] = strings.Split(alpn, ",")
	}
	if sni := query.Get("sni"); sni != "" {
		proxy["sni"] = sni
	}
	if query.Get("disable_sni") == "1" {
		proxy["disable-sni"] = true
	}
	if mode := query.Get("udp_relay_mode"); mode != "" {
		proxy["udp-relay-mode"] = mode
	}
	return proxy, nil
}

func parseAnyTLSURI(raw string, index int) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	proxy, err := basicHostPortProxy(u, "anytls", index)
	if err != nil {
		return nil, err
	}
	username := u.User.Username()
	password, ok := u.User.Password()
	if !ok {
		password = username
	}
	query := u.Query()
	proxy["username"] = username
	proxy["password"] = password
	proxy["sni"] = query.Get("sni")
	proxy["fingerprint"] = query.Get("hpkp")
	proxy["skip-cert-verify"] = query.Get("insecure") == "1"
	proxy["udp"] = true
	return proxy, nil
}

func parseSocksURI(raw string, index int) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	proxy, err := basicHostPortProxy(u, "socks5", index)
	if err != nil {
		return nil, err
	}
	if encoded := u.User.String(); encoded != "" {
		decoded := string(decodeBase64Bytes([]byte(encoded)))
		username, password, _ := strings.Cut(decoded, ":")
		proxy["username"] = username
		proxy["password"] = password
	}
	proxy["skip-cert-verify"] = true
	return proxy, nil
}

func basicHostPortProxy(u *url.URL, proxyType string, index int) (map[string]any, error) {
	return basicHostPortProxyWithDefaultPort(u, proxyType, "", index)
}

func basicHostPortProxyWithDefaultPort(u *url.URL, proxyType, defaultPort string, index int) (map[string]any, error) {
	server := u.Hostname()
	if server == "" {
		return nil, fmt.Errorf("missing server")
	}
	port := u.Port()
	if port == "" {
		port = defaultPort
	}
	if port == "" {
		return nil, fmt.Errorf("missing port")
	}
	name := u.Fragment
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("%s-%s:%s", proxyType, server, port)
	}
	return map[string]any{
		"name":   name,
		"type":   proxyType,
		"server": server,
		"port":   port,
	}, nil
}

func applyTransportOptions(proxy map[string]any, network, host, path, serviceName string) {
	switch network {
	case "http":
		headers := map[string]any{}
		if host != "" {
			headers["Host"] = []string{host}
		}
		if path == "" {
			path = "/"
		}
		proxy["http-opts"] = map[string]any{
			"path":    []string{path},
			"headers": headers,
		}
	case "h2":
		if path == "" {
			path = "/"
		}
		opts := map[string]any{"path": path}
		if host != "" {
			opts["host"] = []string{host}
		}
		proxy["h2-opts"] = opts
	case "ws", "httpupgrade":
		headers := map[string]any{"User-Agent": "Mozilla/5.0"}
		if host != "" {
			headers["Host"] = host
		}
		proxy["ws-opts"] = map[string]any{
			"path":    path,
			"headers": headers,
		}
	case "grpc":
		proxy["grpc-opts"] = map[string]any{"grpc-service-name": serviceName}
	}
}

func decodeURLSafe(value string) string {
	if value == "" {
		return ""
	}
	decoded, err := decodeBase64String(urlSafe(value))
	if err != nil {
		return ""
	}
	return decoded
}

func urlSafe(value string) string {
	value = strings.ReplaceAll(value, "-", "+")
	value = strings.ReplaceAll(value, "_", "/")
	return value
}

func decodeBase64String(value string) (string, error) {
	data, err := decodeBase64BytesStrict([]byte(value))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeBase64Bytes(value []byte) []byte {
	data, err := decodeBase64BytesStrict(value)
	if err != nil {
		return nil
	}
	return data
}

func decodeBase64BytesStrict(value []byte) ([]byte, error) {
	text := strings.TrimSpace(string(value))
	padded := text + strings.Repeat("=", (4-len(text)%4)%4)
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		data, err := enc.DecodeString(padded)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
