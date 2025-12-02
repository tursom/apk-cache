package utils

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// IPMatcher 用于匹配 IP 地址和 CIDR 网段
type IPMatcher struct {
	exemptCIDRs    []*net.IPNet
	trustedProxies []net.IP
}

// NewIPMatcher 创建新的 IP 匹配器
func NewIPMatcher(exemptIPs, trustedProxyIPs string) (*IPMatcher, error) {
	matcher := &IPMatcher{
		exemptCIDRs:    make([]*net.IPNet, 0),
		trustedProxies: make([]net.IP, 0),
	}

	// 解析不需要验证的 IP 网段
	if exemptIPs != "" {
		for cidr := range strings.SplitSeq(exemptIPs, ",") {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR format '%s': %v", cidr, err)
			}
			matcher.exemptCIDRs = append(matcher.exemptCIDRs, ipNet)
		}
	}

	// 解析信任的反向代理 IP
	if trustedProxyIPs != "" {
		for ipStr := range strings.SplitSeq(trustedProxyIPs, ",") {
			ipStr = strings.TrimSpace(ipStr)
			if ipStr == "" {
				continue
			}
			// 尝试解析为 IP 地址
			resolvedIPs := parseIPOrHostname(ipStr)
			if resolvedIPs != nil {
				matcher.trustedProxies = append(matcher.trustedProxies, resolvedIPs...)
			}
		}
	}

	return matcher, nil
}

// IsExemptIP 检查 IP 是否在不需要验证的网段中
func (m *IPMatcher) IsExemptIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	for _, cidr := range m.exemptCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// IsTrustedProxy 检查 IP 是否是信任的反向代理
func (m *IPMatcher) IsTrustedProxy(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	for _, trustedIP := range m.trustedProxies {
		if trustedIP.Equal(ip) {
			return true
		}
	}
	return false
}

// DebugInfo 返回匹配器的调试信息
func (m *IPMatcher) DebugInfo() (exemptCIDRs []string, trustedProxies []string) {
	for _, cidr := range m.exemptCIDRs {
		exemptCIDRs = append(exemptCIDRs, cidr.String())
	}
	for _, ip := range m.trustedProxies {
		trustedProxies = append(trustedProxies, ip.String())
	}
	return
}

// parseIPOrHostname 解析 IP 地址或主机名
func parseIPOrHostname(addr string) []net.IP {
	// 首先尝试解析为 IP 地址
	ip := net.ParseIP(addr)
	if ip != nil {
		return []net.IP{ip}
	}

	// 如果不是 IP 地址，尝试解析为主机名
	resolvedIPs, err := net.LookupIP(addr)
	if err != nil || len(resolvedIPs) == 0 {
		return nil
	}

	// 使用第一个解析到的 IP 地址
	return resolvedIPs
}

// GetRealClientIP 获取真实的客户端 IP，支持 nginx 反向代理
func (m *IPMatcher) GetRealClientIP(r *http.Request) string {
	// 检查 X-Real-IP 头（nginx 常用）
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		// 验证 X-Real-IP 是否来自信任的反向代理
		remoteIP := getRemoteIP(r)
		if m.IsTrustedProxy(remoteIP) {
			return realIP
		}
	}

	// 检查 X-Forwarded-For 头
	forwardedFor := r.Header.Get("X-Forwarded-For")
	if forwardedFor != "" {
		// X-Forwarded-For 格式：client, proxy1, proxy2
		state := 0
		clientIP := ""
	FORWARDED_FOR_LOOP:
		for ip := range strings.SplitSeq(forwardedFor, ",") {
			switch state {
			case 0:
				// 取第一个 IP（客户端 IP）
				clientIP = strings.TrimSpace(ip)
				// 验证整个代理链是否可信
				remoteIP := getRemoteIP(r)
				if !m.IsTrustedProxy(remoteIP) {
					break FORWARDED_FOR_LOOP
				}

				state = 1
			case 1:
				// 检查所有中间代理是否可信
				if !m.IsTrustedProxy(ip) {
					state = 2
					break FORWARDED_FOR_LOOP
				}
			}
		}
		if state == 1 {
			return clientIP
		}
	}

	// 如果没有可信的反向代理头，返回远程 IP
	return getRemoteIP(r)
}

// getRemoteIP 获取远程 IP 地址
func getRemoteIP(r *http.Request) string {
	// 从 RemoteAddr 中提取 IP（格式：IP:port）
	remoteAddr := r.RemoteAddr
	if remoteAddr == "" {
		return ""
	}

	// 分割 IP 和端口
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// 如果没有端口，直接返回
		return remoteAddr
	}
	return host
}
