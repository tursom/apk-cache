package upstream

import (
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// CreateTransport 根据代理地址创建 HTTP Transport
func CreateTransport(proxyAddr string) http.RoundTripper {
	if proxyAddr == "" {
		return nil
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil
	}

	if proxyURL.Scheme == "socks5" {
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		if err != nil {
			return nil
		}
		return &http.Transport{
			Dial: dialer.Dial,
		}
	}

	// HTTP/HTTPS 代理
	return &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
}

// CreateClient 根据代理地址创建 HTTP 客户端
func CreateClient(proxyAddr string, timeout ...time.Duration) *http.Client {
	t := 30 * time.Second
	if len(timeout) > 0 && timeout[0] > 0 {
		t = timeout[0]
	}

	transport := CreateTransport(proxyAddr)
	if transport == nil {
		return &http.Client{Timeout: t}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   t,
	}
}
