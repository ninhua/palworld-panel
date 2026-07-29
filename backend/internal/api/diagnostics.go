package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	diagnosticTimeout       = 15 * time.Second
	diagnosticMaxBodyBytes  = 64 * 1024
	diagnosticMaxCommandLen = 4096
)

type diagnosticHTTPRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type diagnosticShellRequest struct {
	Command string `json:"command"`
	Confirm bool   `json:"confirm"`
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{remaining: limit}
}

func (w *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	if len(data) > w.remaining {
		data = data[:max(w.remaining, 0)]
		w.truncated = true
	}
	if len(data) > 0 {
		_, _ = w.buffer.Write(data)
		w.remaining -= len(data)
	}
	return originalLength, nil
}

func (s Server) diagnosticStatus(c *gin.Context) {
	ok(c, gin.H{
		"http_enabled":  true,
		"shell_enabled": s.cfg.DiagnosticShellEnabled,
		"timeout_ms":    diagnosticTimeout.Milliseconds(),
		"max_output":    diagnosticMaxBodyBytes,
		"platform":      runtime.GOOS,
	})
}

func (s Server) runDiagnosticHTTP(c *gin.Context) {
	var request diagnosticHTTPRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		fail(c, http.StatusBadRequest, "diagnostic_method_invalid", "method must be GET, HEAD, POST, PUT, PATCH, or DELETE")
		return
	}
	if len(request.Body) > diagnosticMaxBodyBytes {
		fail(c, http.StatusRequestEntityTooLarge, "diagnostic_body_too_large", "request body exceeds 64 KiB")
		return
	}
	target, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil {
		fail(c, http.StatusBadRequest, "diagnostic_url_invalid", "URL must be an http or https private-network address without user information")
		return
	}
	if _, err := resolvePrivateHost(c.Request.Context(), target.Hostname()); err != nil {
		fail(c, http.StatusBadRequest, "diagnostic_target_rejected", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), diagnosticTimeout)
	defer cancel()
	outbound, err := http.NewRequestWithContext(ctx, method, target.String(), strings.NewReader(request.Body))
	if err != nil {
		fail(c, http.StatusBadRequest, "diagnostic_request_invalid", err.Error())
		return
	}
	if len(request.Headers) > 32 {
		fail(c, http.StatusBadRequest, "diagnostic_headers_invalid", "at most 32 headers are allowed")
		return
	}
	for name, value := range request.Headers {
		if !validDiagnosticHeader(name, value) {
			fail(c, http.StatusBadRequest, "diagnostic_headers_invalid", "headers contain a forbidden name or invalid value")
			return
		}
		outbound.Header.Set(name, value)
	}

	transport := &http.Transport{
		Proxy:                 nil,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: diagnosticTimeout,
		DialContext:           privateDiagnosticDialer,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   diagnosticTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if _, err := resolvePrivateHost(request.Context(), request.URL.Hostname()); err != nil {
				return err
			}
			return nil
		},
	}
	started := time.Now()
	response, err := client.Do(outbound)
	if err != nil {
		code := "diagnostic_http_failed"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "diagnostic_timeout"
		}
		fail(c, http.StatusBadGateway, code, err.Error())
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, diagnosticMaxBodyBytes+1))
	if err != nil {
		fail(c, http.StatusBadGateway, "diagnostic_response_failed", err.Error())
		return
	}
	truncated := len(body) > diagnosticMaxBodyBytes
	if truncated {
		body = body[:diagnosticMaxBodyBytes]
	}
	ok(c, gin.H{
		"method":      method,
		"url":         target.String(),
		"status":      response.Status,
		"status_code": response.StatusCode,
		"headers":     response.Header,
		"body":        string(body),
		"truncated":   truncated,
		"duration_ms": time.Since(started).Milliseconds(),
	})
}

func (s Server) runDiagnosticShell(c *gin.Context) {
	if !s.cfg.DiagnosticShellEnabled {
		fail(c, http.StatusForbidden, "diagnostic_shell_disabled", "terminal execution is disabled; set PALPANEL_DIAGNOSTIC_SHELL_ENABLED=true and restart PalPanel to enable it")
		return
	}
	var request diagnosticShellRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	command := strings.TrimSpace(request.Command)
	if command == "" || len(command) > diagnosticMaxCommandLen {
		fail(c, http.StatusBadRequest, "diagnostic_command_invalid", "command must contain 1 to 4096 characters")
		return
	}
	if !request.Confirm {
		fail(c, http.StatusBadRequest, "diagnostic_confirmation_required", "explicit confirmation is required")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), diagnosticTimeout)
	defer cancel()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		process = exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
	} else {
		process = exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	}
	process.Dir = s.cfg.RuntimeRoot
	output := newLimitedBuffer(diagnosticMaxBodyBytes)
	process.Stdout = output
	process.Stderr = output
	started := time.Now()
	err := process.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
	}
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	setAuditSuccess(c, err == nil)
	ok(c, gin.H{
		"command":     command,
		"output":      output.buffer.String(),
		"exit_code":   exitCode,
		"success":     err == nil,
		"timed_out":   timedOut,
		"truncated":   output.truncated,
		"duration_ms": time.Since(started).Milliseconds(),
		"error":       commandErrorMessage(err, timedOut),
	})
}

func commandErrorMessage(err error, timedOut bool) string {
	if timedOut {
		return "command exceeded the 15 second timeout"
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func validDiagnosticHeader(name, value string) bool {
	name = http.CanonicalHeaderKey(strings.TrimSpace(name))
	if name == "" || strings.ContainsAny(value, "\r\n") {
		return false
	}
	switch name {
	case "Host", "Connection", "Content-Length", "Proxy-Authorization", "Proxy-Connection", "Transfer-Encoding", "Upgrade":
		return false
	default:
		return true
	}
}

func privateDiagnosticDialer(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := resolvePrivateHost(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	var lastErr error
	for _, address := range addresses {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func resolvePrivateHost(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return nil, errors.New("target host is empty")
	}
	var addresses []net.IP
	if parsed := net.ParseIP(host); parsed != nil {
		addresses = []net.IP{parsed}
	} else {
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve target host: %w", err)
		}
		for _, address := range resolved {
			addresses = append(addresses, address.IP)
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("target host did not resolve to an address")
	}
	for _, address := range addresses {
		if address.IsUnspecified() || (!address.IsPrivate() && !address.IsLoopback()) {
			return nil, fmt.Errorf("target address %s is not loopback or private", address)
		}
	}
	return addresses, nil
}
