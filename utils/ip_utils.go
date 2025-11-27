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
		cidrs := strings.Split(exemptIPs, ",")
		for _, cidr := range cidrs {
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
		ips := strings.Split(trustedProxyIPs, ",")
		for _, ipStr := range ips {
			ipStr = strings.TrimSpace(ipStr)
			if ipStr == "" {
				continue
			}
			ip := net.ParseIP(ipStr)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP address '%s'", ipStr)
			}
			matcher.trustedProxies = append(matcher.trustedProxies, ip)
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
		ips := strings.Split(forwardedFor, ",")
		if len(ips) > 0 {
			// 取第一个 IP（客户端 IP）
			clientIP := strings.TrimSpace(ips[0])
			// 验证整个代理链是否可信
			remoteIP := getRemoteIP(r)
			if m.IsTrustedProxy(remoteIP) {
				// 检查所有中间代理是否可信
				allProxiesTrusted := true
				for i := 1; i < len(ips); i++ {
					proxyIP := strings.TrimSpace(ips[i])
					if !m.IsTrustedProxy(proxyIP) {
						allProxiesTrusted = false
						break
					}
				}
				if allProxiesTrusted {
					return clientIP
				}
			}
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
