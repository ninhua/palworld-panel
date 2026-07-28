package mods

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type staticResolver map[string][]net.IPAddr

func (r staticResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	return r[host], nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSafeDownloaderRejectsNonPublicDestinationsAndCredentials(t *testing.T) {
	downloader := newSafeDownloader()
	downloader.resolver = staticResolver{
		"public.example":  {{IP: net.ParseIP("8.8.8.8")}},
		"private.example": {{IP: net.ParseIP("10.0.0.2")}},
		"mixed.example":   {{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}},
		"docs.example":    {{IP: net.ParseIP("203.0.113.20")}},
	}
	tests := []string{
		"ftp://public.example/mod.zip",
		"https://user:password@public.example/mod.zip",
		"https://private.example/mod.zip",
		"https://mixed.example/mod.zip",
		"https://docs.example/mod.zip",
		"https://127.0.0.1/mod.zip",
		"https://[::1]/mod.zip",
		"https://[64:ff9b::a00:1]/mod.zip",
		"https://[2002:0a00:0001::]/mod.zip",
	}
	for _, rawURL := range tests {
		parsed, err := parseRemoteURL(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		if err := downloader.validateURL(context.Background(), parsed); err == nil {
			t.Errorf("expected %s to be rejected", rawURL)
		}
	}
	for _, rawURL := range []string{"http://public.example/mod.zip", "https://public.example/mod.zip"} {
		parsed, err := parseRemoteURL(rawURL)
		if err != nil || downloader.validateURL(context.Background(), parsed) != nil {
			t.Fatalf("public URL %q was rejected: %v", rawURL, err)
		}
	}
	transport, ok := downloader.client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("safe downloader must ignore proxy environment variables")
	}
}

func TestSafeDownloaderUsesOnlyExplicitManagedProxy(t *testing.T) {
	downloader := newSafeDownloader(func() (string, error) {
		return "http://proxy-user:proxy-secret@127.0.0.1:7890", nil
	})
	client, err := downloader.clientForDownload()
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatal("managed download proxy was not configured")
	}
	request, _ := http.NewRequest(http.MethodGet, "https://public.example/mod.zip", nil)
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil || proxyURL.Host != "127.0.0.1:7890" || proxyURL.User == nil {
		t.Fatalf("proxy URL = %#v", proxyURL)
	}
}

func TestSafeDownloaderAllowsExactlyFiveRedirects(t *testing.T) {
	downloader := newSafeDownloader()
	downloader.resolver = staticResolver{"public.example": {{IP: net.ParseIP("8.8.8.8")}}}
	request := &http.Request{URL: &url.URL{Scheme: "https", Host: "public.example", Path: "/mod.zip"}}
	for redirects := 1; redirects <= maxRedirects; redirects++ {
		previous := make([]*http.Request, redirects)
		if err := downloader.client.CheckRedirect(request, previous); err != nil {
			t.Fatalf("redirect %d was rejected: %v", redirects, err)
		}
	}
	if err := downloader.client.CheckRedirect(request, make([]*http.Request, maxRedirects+1)); err == nil {
		t.Fatal("sixth redirect was accepted")
	}
}

func TestSafeDownloaderRevalidatesRedirectsAndEnforcesSize(t *testing.T) {
	downloader := newSafeDownloader()
	downloader.resolver = staticResolver{
		"public.example":  {{IP: net.ParseIP("8.8.8.8")}},
		"private.example": {{IP: net.ParseIP("192.168.1.5")}},
	}
	downloader.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "public.example" {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://private.example/mod.zip"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		}
		return nil, nil
	})
	if _, err := downloader.Download(context.Background(), "https://public.example/mod.zip", filepath.Join(t.TempDir(), "redirect.zip"), 100); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("private redirect error = %v", err)
	}

	downloader.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader("123456")),
			ContentLength: 6,
			Request:       request,
		}, nil
	})
	destination := filepath.Join(t.TempDir(), "large.zip")
	if _, err := downloader.Download(context.Background(), "https://public.example/mod.zip", destination, 5); err == nil {
		t.Fatal("expected oversized download to fail")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("oversized destination was retained: %v", err)
	}
}

func parseRemoteURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}
