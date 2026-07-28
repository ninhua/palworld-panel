package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDockerHelperNormalizationAndFormatting(t *testing.T) {
	if got := normalizeDockerSource(" ALIYUN "); got != "aliyun" {
		t.Fatalf("normalizeDockerSource() = %q", got)
	}
	if got := normalizeDockerSource("unknown"); got != "auto" {
		t.Fatalf("normalizeDockerSource(unknown) = %q", got)
	}
	if got := normalizeDockerMirror(" DOCKERPROXY_NET "); got != "dockerproxy_net" {
		t.Fatalf("normalizeDockerMirror() = %q", got)
	}
	if got := dockerProbeURL("apt", "https://example.test/"); got != "https://example.test/gpg" {
		t.Fatalf("dockerProbeURL(apt) = %q", got)
	}
	if got := dockerProbeURL("dnf", "https://example.test/"); got != "https://example.test/docker-ce.repo" {
		t.Fatalf("dockerProbeURL(dnf) = %q", got)
	}
	if got := shellQuote("Pal'Panel"); got != "'Pal'\"'\"'Panel'" {
		t.Fatalf("shellQuote() = %q", got)
	}
	if got := limitString(" 123456 ", 3); got != "123...(truncated)" {
		t.Fatalf("limitString() = %q", got)
	}
	if boolInt(true) != 1 || boolInt(false) != 0 || minInt(2, 3) != 2 || minInt(4, 3) != 3 {
		t.Fatal("boolean or integer helpers returned unexpected values")
	}
}

func TestReadOSReleaseAndMirrorParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "os-release")
	body := "# comment\nID=\"ubuntu\"\nVERSION_CODENAME=noble\nINVALID\nEMPTY=''\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readOSRelease(path); !reflect.DeepEqual(got, map[string]string{
		"ID":               "ubuntu",
		"VERSION_CODENAME": "noble",
		"EMPTY":            "",
	}) {
		t.Fatalf("readOSRelease() = %#v", got)
	}
	if got := readOSRelease(filepath.Join(t.TempDir(), "missing")); len(got) != 0 {
		t.Fatalf("readOSRelease(missing) = %#v", got)
	}

	mirrors, err := parseDockerDaemonMirrors([]byte(`{"registry-mirrors":[" https://one/ ",7,"","https://two"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mirrors, []string{"https://one", "https://two"}) {
		t.Fatalf("parseDockerDaemonMirrors() = %#v", mirrors)
	}
	if _, err := parseDockerDaemonMirrors([]byte(`{`)); err == nil {
		t.Fatal("parseDockerDaemonMirrors() accepted invalid JSON")
	}
	merged := mergeMirrorLists([]string{"https://new/", "https://same"}, []string{"https://same/", "", "https://old"})
	if !reflect.DeepEqual(merged, []string{"https://new", "https://same", "https://old"}) {
		t.Fatalf("mergeMirrorLists() = %#v", merged)
	}
}

func TestDockerSourceProbes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fallback":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/registry/v2/":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer server.Close()

	if result := probeDockerSource(context.Background(), ""); result.Error != "empty probe URL" {
		t.Fatalf("empty source probe = %#v", result)
	}
	if result := probeDockerSource(context.Background(), server.URL+"/fallback"); !result.Available {
		t.Fatalf("fallback source probe = %#v", result)
	}
	if result := probeDockerSource(context.Background(), server.URL+"/failure"); result.Error != "HTTP 502" {
		t.Fatalf("failed source probe = %#v", result)
	}
	if result := probeDockerRegistryMirror(context.Background(), server.URL+"/registry"); !result.Available {
		t.Fatalf("registry probe = %#v", result)
	}
	if result := probeDockerRegistryMirror(context.Background(), ""); result.Error != "empty mirror URL" {
		t.Fatalf("empty registry probe = %#v", result)
	}
}

func TestDockerPermissionAndDistroHelpers(t *testing.T) {
	if !dockerDetectPermissionDenied(errors.New("permission denied"), []byte("/var/run/docker.sock")) {
		t.Fatal("docker socket permission error was not detected")
	}
	if dockerDetectPermissionDenied(nil, []byte("permission denied /var/run/docker.sock")) {
		t.Fatal("nil error was detected as a permission error")
	}
	if distro, ok := dockerRepoDistro(HostCapabilities{OS: "linux", DistroID: "AlmaLinux"}); !ok || distro != "centos" {
		t.Fatalf("dockerRepoDistro(AlmaLinux) = %q, %v", distro, ok)
	}
	if _, ok := dockerRepoDistro(HostCapabilities{OS: "windows", DistroID: "ubuntu"}); ok {
		t.Fatal("dockerRepoDistro accepted a non-Linux host")
	}
	if got := strings.TrimSpace((&DockerInstallError{Msg: "failed"}).Error()); got != "failed" {
		t.Fatalf("DockerInstallError.Error() = %q", got)
	}
}
