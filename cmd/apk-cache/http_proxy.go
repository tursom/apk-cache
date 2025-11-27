package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// cacheMissReader 用于在读取时统计未命中缓存的字节数
type cacheMissReader struct {
	reader     io.Reader
	totalBytes int64
}

func (cr *cacheMissReader) Read(p []byte) (int, error) {
	n, err := cr.reader.Read(p)
	if n > 0 {
		cacheMissBytes.Add(float64(n))
	}
	return n, err
}

// handleProxyRequest 处理HTTP代理请求
func handleProxyRequest(w http.ResponseWriter, r *http.Request) {
	log.Println(t("HTTPProxyHandlerReceived", map[string]any{
		"Method": r.Method,
		"Path":   r.URL.Path,
	}))
	// 检查是否是CONNECT方法（HTTPS代理）
	if r.Method == http.MethodConnect {
		handleProxyHTTPS(w, r)
		return
	}

	// 处理普通HTTP请求
	handleProxyHTTP(w, r)
}

// handleProxyHTTPS 处理HTTPS代理请求（CONNECT方法）
func handleProxyHTTPS(w http.ResponseWriter, r *http.Request) {
	log.Println(t("HTTPSProxyRequest", map[string]any{
		"Method": r.Method,
		"Host":   r.Host,
	}))

	// 获取目标主机
	host := r.Host
	if host == "" {
		http.Error(w, "Missing target host", http.StatusBadRequest)
		return
	}

	// 确保host包含端口号
	if !strings.Contains(host, ":") {
		// 对于HTTPS，默认端口是443
		host = host + ":443"
		log.Println(t("HTTPSProxyAddedDefaultPort", map[string]any{"Target": host}))
	}

	// 建立到目标服务器的连接
	var targetConn net.Conn
	var err error

	// 尝试使用上游服务器的代理配置
	server := upstreamManager.GetHealthyServer()
	if server != nil && server.GetProxy() != "" {
		// 使用上游服务器的代理
		targetConn, err = proxyDialWithProxy(host, server.GetProxy())
	} else if *proxyURL != "" {
		// 使用全局代理配置
		targetConn, err = proxyDialWithProxy(host, *proxyURL)
	} else {
		// 直接连接
		targetConn, err = net.DialTimeout("tcp", host, 30*time.Second)
	}

	if err != nil {
		log.Println(t("HTTPSProxyConnectFailed", map[string]any{
			"Host":  host,
			"Error": err,
		}))
		http.Error(w, fmt.Sprintf("Failed to connect to %s: %v", host, err), http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	// 获取客户端连接
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, fmt.Sprintf("Hijacking failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// 告诉客户端连接已建立
	// 对于CONNECT请求，需要发送特定格式的响应
	fmt.Fprintf(clientConn, "HTTP/1.1 200 Connection Established\r\n")
	fmt.Fprintf(clientConn, "Proxy-Agent: apk-cache\r\n")
	fmt.Fprintf(clientConn, "\r\n")

	// 双向转发数据
	// 注意：对于HTTPS代理，代理服务器不应该进行TLS握手
	// 客户端会直接与目标服务器进行TLS握手
	go func() {
		io.Copy(targetConn, clientConn)
		targetConn.Close()
	}()
	io.Copy(clientConn, targetConn)
}

// handleProxyHTTP 处理HTTP代理请求
func handleProxyHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println(t("HTTPProxyRequest", map[string]any{
		"Method": r.Method,
		"URL":    r.URL.String(),
		"Host":   r.Host,
	}))

	switch detectPackageTypeFast(r.URL.Path) {
	case PackageTypeAPT:
		// 检查是否是APT协议请求，如果是则使用缓存逻辑
		handleAPTProxy(w, r)
	case PackageTypeAPK:
		// 检查是否是APK协议请求，如果是则使用现有的缓存逻辑
		proxyHandler(w, r)
	default:
		// 对于非APK/APT请求，直接代理转发
		proxyForwardHTTP(w, r)
	}
}

// proxyForwardHTTP 转发HTTP请求到上游服务器
func proxyForwardHTTP(w http.ResponseWriter, r *http.Request) {
	cacheMisses.Add(1)

	// 转发请求
	resp, err := proxyForwardRequest(r)
	if err != nil {
		log.Println(t("HTTPProxyForwardFailed", map[string]any{
			"Error": err,
		}))
		http.Error(w, fmt.Sprintf("Failed to forward request: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 复制响应头
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// 设置响应状态码
	w.WriteHeader(resp.StatusCode)

	// 复制响应体并计算大小
	reader := &cacheMissReader{reader: resp.Body}
	_, err = io.Copy(w, reader)
	if err != nil {
		log.Println(t("HTTPProxyCopyResponseFailed", map[string]any{
			"Error": err,
		}))
	}
}

// proxyDialWithProxy 通过代理建立连接
func proxyDialWithProxy(host, proxyAddr string) (net.Conn, error) {
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, err
	}

	switch proxyURL.Scheme {
	case "http", "https":
		// HTTP代理
		return proxyDialHTTP(host, proxyURL)
	case "socks5":
		// SOCKS5代理
		return proxyDialSOCKS5(host, proxyURL)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
}

// proxyDialHTTP 通过HTTP代理建立连接
func proxyDialHTTP(host string, proxyURL *url.URL) (net.Conn, error) {
	// 连接到代理服务器
	proxyConn, err := net.DialTimeout("tcp", proxyURL.Host, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to proxy %s: %v", proxyURL.Host, err)
	}

	// 发送CONNECT请求
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: apk-cache-proxy\r\nProxy-Connection: Keep-Alive\r\n\r\n", host, host)
	if _, err := proxyConn.Write([]byte(connectReq)); err != nil {
		proxyConn.Close()
		return nil, fmt.Errorf("failed to send CONNECT request: %v", err)
	}

	// 读取代理响应
	reader := bufio.NewReader(proxyConn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		proxyConn.Close()
		return nil, fmt.Errorf("failed to read proxy response: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		proxyConn.Close()
		return nil, fmt.Errorf("proxy returned status: %s", resp.Status)
	}

	log.Println(t("ProxyConnectionEstablished", map[string]any{
		"Proxy":  proxyURL.Host,
		"Target": host,
	}))

	return proxyConn, nil
}

// proxyDialSOCKS5 通过SOCKS5代理建立连接
func proxyDialSOCKS5(host string, proxyURL *url.URL) (net.Conn, error) {
	dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
	if err != nil {
		return nil, err
	}
	return dialer.Dial("tcp", host)
}

// proxyIsProxyRequest 检查是否是代理请求
func proxyIsProxyRequest(r *http.Request) bool {
	// 检查是否是CONNECT方法（HTTPS代理）
	if r.Method == http.MethodConnect {
		return true
	}

	// 检查是否是完整的URL（HTTP代理）
	if r.URL.Scheme != "" || r.URL.Host != "" {
		return true
	}

	// 检查是否包含代理相关的头
	if r.Header.Get("Proxy-Connection") != "" {
		return true
	}

	// 检查是否是绝对URL路径（HTTP代理的常见特征）
	if strings.HasPrefix(r.URL.Path, "http://") || strings.HasPrefix(r.URL.Path, "https://") {
		return true
	}

	return false
}

// proxyBuildTargetURL 构建目标URL，处理代理请求
func proxyBuildTargetURL(r *http.Request) (*url.URL, error) {
	// 构建目标URL - 对于代理请求，使用请求中的完整URL
	var targetURL *url.URL
	var err error

	// 这是代理请求，使用请求中的完整URL信息
	if r.URL.Scheme != "" && r.URL.Host != "" {
		// 直接使用请求中的URL
		targetURL = r.URL
	} else if strings.HasPrefix(r.URL.Path, "http://") || strings.HasPrefix(r.URL.Path, "https://") {
		// 这是代理格式的请求，使用路径作为完整URL
		targetURL, err = url.Parse(r.URL.Path)
		if err != nil {
			return nil, fmt.Errorf("invalid target URL: %v", err)
		}
	} else {
		// 对于代理请求但没有完整URL的情况，使用Host头构建URL
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		targetURL = &url.URL{
			Scheme: scheme,
			Host:   r.Host,
			Path:   r.URL.Path,
		}
	}

	return targetURL, nil
}

// proxyForwardRequest 转发HTTP请求到上游服务器
func proxyForwardRequest(r *http.Request) (*http.Response, error) {
	// 获取健康的上游服务器
	server := upstreamManager.GetHealthyServer()
	var proxy string
	if server != nil {
		proxy = server.GetProxy()
	} else {
		// 没有健康的上游服务器时，使用全局代理配置
		proxy = *proxyURL
	}

	// 构建目标URL
	targetURL, err := proxyBuildTargetURL(r)
	if err != nil {
		return nil, err
	}

	log.Println(t("ForwardingToUpstream", map[string]any{"Method": r.Method, "URL": targetURL.String()}))

	// 创建HTTP客户端
	client := createHTTPClientForUpstream(proxy)

	// 复制原始请求
	req, err := http.NewRequest(r.Method, targetURL.String(), r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// 复制请求头
	for key, values := range r.Header {
		// 跳过代理相关的头
		if strings.EqualFold(key, "Proxy-Connection") {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// 设置正确的Host头
	req.Host = targetURL.Host

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to forward request to %s: %v", targetURL.String(), err)
	}

	return resp, nil
}
