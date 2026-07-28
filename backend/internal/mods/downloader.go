package mods

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/networkproxy"
)

const maxRedirects = 5

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type safeDownloader struct {
	resolver ipResolver
	client   *http.Client
	proxyURL func() (string, error)
}

func newSafeDownloader(proxyResolvers ...func() (string, error)) *safeDownloader {
	downloader := &safeDownloader{resolver: net.DefaultResolver}
	if len(proxyResolvers) > 0 {
		downloader.proxyURL = proxyResolvers[0]
	}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	transport.DialContext = downloader.dialContext
	downloader.client = &http.Client{
		Transport: transport,
		Timeout:   5 * time.Minute,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) > maxRedirects {
				return errors.New("too many redirects")
			}
			return downloader.validateURL(request.Context(), request.URL)
		},
	}
	return downloader
}

func (d *safeDownloader) clientForDownload() (*http.Client, error) {
	if d.proxyURL == nil {
		return d.client, nil
	}
	rawProxy, err := d.proxyURL()
	if err != nil {
		return nil, fmt.Errorf("read managed download proxy: %w", err)
	}
	if strings.TrimSpace(rawProxy) == "" {
		return d.client, nil
	}
	transport, err := networkproxy.Transport(rawProxy)
	if err != nil {
		return nil, err
	}
	transport.ForceAttemptHTTP2 = true
	transport.MaxIdleConns = 10
	transport.IdleConnTimeout = 30 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport, Timeout: d.client.Timeout, CheckRedirect: d.client.CheckRedirect}, nil
}

func (d *safeDownloader) Download(ctx context.Context, rawURL, destination string, limit int64) (int64, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return 0, fmt.Errorf("parse download URL: %w", err)
	}
	if err := d.validateURL(ctx, parsed); err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/zip, application/octet-stream, application/json;q=0.9")
	request.Header.Set("User-Agent", "PalPanel-Mod-Importer/1")
	client, err := d.clientForDownload()
	if err != nil {
		return 0, err
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("download failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	if limit <= 0 {
		return 0, errors.New("download limit must be positive")
	}
	if response.ContentLength > limit {
		return 0, fmt.Errorf("download exceeds the %d byte limit", limit)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return 0, closeErr
	}
	if written > limit {
		_ = os.Remove(destination)
		return 0, fmt.Errorf("download exceeds the %d byte limit", limit)
	}
	return written, nil
}

func (d *safeDownloader) validateURL(ctx context.Context, parsed *url.URL) error {
	if parsed == nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return errors.New("only public HTTP(S) URLs are allowed")
	}
	if parsed.User != nil {
		return errors.New("URLs containing credentials are not allowed")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return errors.New("download URL has no host")
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return errors.New("download URL has an invalid port")
		}
	}
	addresses, err := d.lookup(ctx, host)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if !publicAddress(address) {
			return fmt.Errorf("download host resolves to a non-public address: %s", address)
		}
	}
	return nil
}

func (d *safeDownloader) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address.Unmap()}, nil
	}
	resolved, err := d.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve download host: %w", err)
	}
	if len(resolved) == 0 {
		return nil, errors.New("download host did not resolve")
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, item := range resolved {
		address, ok := netip.AddrFromSlice(item.IP)
		if !ok {
			return nil, errors.New("download host returned an invalid address")
		}
		addresses = append(addresses, address.Unmap())
	}
	return addresses, nil
}

func (d *safeDownloader) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := d.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, resolved := range addresses {
		if !publicAddress(resolved) {
			return nil, fmt.Errorf("download host resolves to a non-public address: %s", resolved)
		}
	}
	dialer := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	var lastError error
	for _, resolved := range addresses {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastError = dialErr
	}
	return nil, lastError
}

var reservedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return false
	}
	for _, network := range reservedNetworks {
		if network.Contains(address) {
			return false
		}
	}
	return true
}
