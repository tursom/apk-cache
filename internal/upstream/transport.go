package upstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const DefaultDialTimeout = 30 * time.Second

func CreateTransport(proxyAddr string) *http.Transport {
	transport := &http.Transport{}
	if proxyAddr == "" {
		return transport
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return transport
	}

	switch proxyURL.Scheme {
	case "socks5":
		dialer, err := proxy.FromURL(proxyURL, &net.Dialer{Timeout: DefaultDialTimeout})
		if err != nil {
			return transport
		}
		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			transport.Proxy = nil
			transport.DialContext = contextDialer.DialContext
			return transport
		}
		transport.Proxy = nil
		transport.DialContext = func(_ context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(network, address)
		}
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
		if value := ProxyAuthorizationValue(proxyURL); value != "" {
			transport.ProxyConnectHeader = http.Header{
				"Proxy-Authorization": []string{value},
			}
		}
	}
	return transport
}

func DialContextViaProxy(ctx context.Context, proxyAddr, network, address string, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}
	if proxyAddr == "" {
		return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, address)
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, err
	}
	switch proxyURL.Scheme {
	case "socks5":
		dialer, err := proxy.FromURL(proxyURL, &net.Dialer{Timeout: timeout})
		if err != nil {
			return nil, err
		}
		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			return contextDialer.DialContext(ctx, network, address)
		}
		return dialer.Dial(network, address)
	case "http", "https":
		return dialViaHTTPProxy(ctx, proxyURL, network, address, timeout)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", proxyURL.Scheme)
	}
}

func ProxyAuthorizationValue(proxyURL *url.URL) string {
	if proxyURL == nil || proxyURL.User == nil {
		return ""
	}
	user := proxyURL.User.Username()
	pass, _ := proxyURL.User.Password()
	if user == "" && pass == "" {
		return ""
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func dialViaHTTPProxy(ctx context.Context, proxyURL *url.URL, network, address string, timeout time.Duration) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("http proxy only supports tcp, got %q", network)
	}
	conn, err := dialProxyServer(ctx, proxyURL, timeout)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodConnect, "http://"+address, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	safeAddress := strings.ReplaceAll(strings.ReplaceAll(address, "\r", ""), "\n", "")
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", safeAddress, safeAddress); err != nil {
		conn.Close()
		return nil, err
	}
	if value := ProxyAuthorizationValue(proxyURL); value != "" {
		if _, err := fmt.Fprintf(conn, "Proxy-Authorization: %s\r\n", value); err != nil {
			conn.Close()
			return nil, err
		}
	}
	if _, err := fmt.Fprint(conn, "\r\n"); err != nil {
		conn.Close()
		return nil, err
	}

	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("proxy CONNECT to %s failed: %s", address, response.Status)
	}
	return conn, nil
}

func dialProxyServer(ctx context.Context, proxyURL *url.URL, timeout time.Duration) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout}
	switch proxyURL.Scheme {
	case "http":
		return dialer.DialContext(ctx, "tcp", proxyURL.Host)
	case "https":
		return (&tls.Dialer{
			NetDialer: dialer,
			Config: &tls.Config{
				ServerName: proxyURL.Hostname(),
				MinVersion: tls.VersionTLS12,
			},
		}).DialContext(ctx, "tcp", proxyURL.Host)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", proxyURL.Scheme)
	}
}
