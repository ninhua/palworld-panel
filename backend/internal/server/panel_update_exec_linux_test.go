//go:build linux

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbePanelExecHealthRequiresReadyAndTargetVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/ready":
			_, _ = writer.Write([]byte(`{"ok":true,"data":{"status":"ready"}}`))
		case "/api/patch/info":
			_, _ = writer.Write([]byte(`{"ok":true,"data":{"build":{"version":"v1.3.0-custom.0.8.31"}}}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	if err := probePanelExecHealth(server.Client(), server.URL, "v1.3.0-custom.0.8.31"); err != nil {
		t.Fatal(err)
	}
	if err := probePanelExecHealth(server.Client(), server.URL, "v1.3.0-custom.0.8.32"); err == nil {
		t.Fatal("expected target version mismatch")
	}
}
