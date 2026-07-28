package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"palpanel/internal/db"
)

const webDAVConfigFilename = "webdav.json"

type WebDAVConfig struct {
	Enabled            bool   `json:"enabled"`
	BaseURL            string `json:"base_url"`
	Username           string `json:"username"`
	RemotePath         string `json:"remote_path"`
	UploadAfterBackup  bool   `json:"upload_after_backup"`
	PasswordConfigured bool   `json:"password_configured"`
}

type WebDAVConfigUpdate struct {
	Enabled           *bool   `json:"enabled,omitempty"`
	BaseURL           *string `json:"base_url,omitempty"`
	Username          *string `json:"username,omitempty"`
	Password          *string `json:"password,omitempty"`
	ClearPassword     bool    `json:"clear_password,omitempty"`
	RemotePath        *string `json:"remote_path,omitempty"`
	UploadAfterBackup *bool   `json:"upload_after_backup,omitempty"`
}

type webDAVStoredConfig struct {
	Enabled           bool   `json:"enabled"`
	BaseURL           string `json:"base_url"`
	Username          string `json:"username,omitempty"`
	Password          string `json:"password,omitempty"`
	RemotePath        string `json:"remote_path"`
	UploadAfterBackup bool   `json:"upload_after_backup"`
}

func (m Manager) WebDAVConfig(_ context.Context) (WebDAVConfig, error) {
	stored, err := m.readWebDAVConfig()
	if err != nil {
		return WebDAVConfig{}, err
	}
	return publicWebDAVConfig(stored), nil
}

func (m Manager) UpdateWebDAVConfig(_ context.Context, update WebDAVConfigUpdate) (WebDAVConfig, error) {
	stored, err := m.mergedWebDAVConfig(update)
	if err != nil {
		return WebDAVConfig{}, err
	}
	if err := m.writeWebDAVConfig(stored); err != nil {
		return WebDAVConfig{}, err
	}
	return publicWebDAVConfig(stored), nil
}

func (m Manager) TestWebDAV(ctx context.Context, update WebDAVConfigUpdate) error {
	stored, err := m.mergedWebDAVConfig(update)
	if err != nil {
		return err
	}
	if strings.TrimSpace(stored.BaseURL) == "" {
		return errors.New("WebDAV URL is required")
	}
	request, err := m.newWebDAVRequest(ctx, stored, "PROPFIND", stored.BaseURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Depth", "0")
	response, err := m.webDAVClient().Do(request)
	if err != nil {
		return fmt.Errorf("WebDAV connection failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return webDAVStatusError("WebDAV connection test", response)
	}
	return nil
}

func (m Manager) UploadBackupToWebDAV(ctx context.Context, name string) (db.Job, error) {
	backupPath, cleanName, err := m.backupPath(name)
	if err != nil {
		return db.Job{}, err
	}
	if !fileExists(backupPath) {
		return db.Job{}, errors.New("backup not found")
	}
	stored, err := m.readWebDAVConfig()
	if err != nil {
		return db.Job{}, err
	}
	if !stored.Enabled {
		return db.Job{}, errors.New("WebDAV upload is disabled")
	}
	return m.startJob(ctx, "webdav_upload", "queued WebDAV upload", func(jobCtx context.Context, jobID string) {
		m.update(jobID, "running", 15, "preparing WebDAV upload", "")
		if err := m.uploadFileToWebDAV(jobCtx, stored, backupPath, cleanName); err != nil {
			m.update(jobID, "failed", 40, "WebDAV upload failed; local backup retained", err.Error())
			return
		}
		m.update(jobID, "completed", 100, "backup uploaded to WebDAV: "+cleanName, "")
	})
}

func (m Manager) maybeUploadBackup(ctx context.Context, jobID string, backup BackupInfo) error {
	stored, err := m.readWebDAVConfig()
	if err != nil {
		return err
	}
	if !stored.Enabled || !stored.UploadAfterBackup {
		return nil
	}
	m.update(jobID, "running", 70, "uploading backup to WebDAV", "")
	return m.uploadFileToWebDAV(ctx, stored, backup.Path, backup.Name)
}

func (m Manager) mergedWebDAVConfig(update WebDAVConfigUpdate) (webDAVStoredConfig, error) {
	stored, err := m.readWebDAVConfig()
	if err != nil {
		return webDAVStoredConfig{}, err
	}
	if update.Enabled != nil {
		stored.Enabled = *update.Enabled
	}
	if update.BaseURL != nil {
		stored.BaseURL = *update.BaseURL
	}
	if update.Username != nil {
		stored.Username = *update.Username
	}
	if update.Password != nil && *update.Password != "" {
		stored.Password = *update.Password
	}
	if update.ClearPassword {
		stored.Password = ""
	}
	if update.RemotePath != nil {
		stored.RemotePath = *update.RemotePath
	}
	if update.UploadAfterBackup != nil {
		stored.UploadAfterBackup = *update.UploadAfterBackup
	}
	return normalizeWebDAVConfig(stored)
}

func (m Manager) readWebDAVConfig() (webDAVStoredConfig, error) {
	config := webDAVStoredConfig{RemotePath: "PalPanel"}
	body, err := os.ReadFile(m.webDAVConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return webDAVStoredConfig{}, fmt.Errorf("read WebDAV config: %w", err)
	}
	if err := json.Unmarshal(body, &config); err != nil {
		return webDAVStoredConfig{}, fmt.Errorf("decode WebDAV config: %w", err)
	}
	return normalizeWebDAVConfig(config)
}

func (m Manager) writeWebDAVConfig(config webDAVStoredConfig) error {
	configPath := m.webDAVConfigPath()
	if err := m.cfg.ValidateManagedPath(configPath, false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("create WebDAV config directory: %w", err)
	}
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary := configPath + ".tmp"
	if err := os.WriteFile(temporary, body, 0o600); err != nil {
		return fmt.Errorf("write WebDAV config: %w", err)
	}
	if err := os.Chmod(temporary, 0o600); err != nil && runtime.GOOS != "windows" {
		_ = os.Remove(temporary)
		return fmt.Errorf("secure WebDAV config: %w", err)
	}
	if err := os.Rename(temporary, configPath); err != nil {
		if runtime.GOOS != "windows" {
			_ = os.Remove(temporary)
			return fmt.Errorf("replace WebDAV config: %w", err)
		}
		if removeErr := os.Remove(configPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			_ = os.Remove(temporary)
			return fmt.Errorf("replace WebDAV config: %w", err)
		}
		if retryErr := os.Rename(temporary, configPath); retryErr != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("replace WebDAV config: %w", retryErr)
		}
	}
	return nil
}

func (m Manager) webDAVConfigPath() string {
	return filepath.Join(m.cfg.DataDir, "secrets", webDAVConfigFilename)
}

func normalizeWebDAVConfig(config webDAVStoredConfig) (webDAVStoredConfig, error) {
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.Username = strings.TrimSpace(config.Username)
	config.RemotePath = strings.Trim(strings.ReplaceAll(strings.TrimSpace(config.RemotePath), "\\", "/"), "/")
	if config.RemotePath == "" {
		config.RemotePath = "PalPanel"
	}
	for _, segment := range strings.Split(config.RemotePath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return webDAVStoredConfig{}, errors.New("WebDAV remote path contains an unsafe segment")
		}
	}
	if config.BaseURL == "" {
		if config.Enabled || config.UploadAfterBackup {
			return webDAVStoredConfig{}, errors.New("WebDAV URL is required when upload is enabled")
		}
		return config, nil
	}
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return webDAVStoredConfig{}, errors.New("WebDAV URL must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return webDAVStoredConfig{}, errors.New("WebDAV URL must not include credentials, query parameters, or fragments")
	}
	return config, nil
}

func publicWebDAVConfig(config webDAVStoredConfig) WebDAVConfig {
	return WebDAVConfig{
		Enabled:            config.Enabled,
		BaseURL:            config.BaseURL,
		Username:           config.Username,
		RemotePath:         config.RemotePath,
		UploadAfterBackup:  config.UploadAfterBackup,
		PasswordConfigured: config.Password != "",
	}
}

func (m Manager) uploadFileToWebDAV(ctx context.Context, config webDAVStoredConfig, localPath, name string) error {
	if err := m.ensureWebDAVDirectories(ctx, config); err != nil {
		return err
	}
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local backup: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	remoteURL, err := webDAVURL(config.BaseURL, config.RemotePath, name)
	if err != nil {
		return err
	}
	request, err := m.newWebDAVRequest(ctx, config, http.MethodPut, remoteURL, file)
	if err != nil {
		return err
	}
	request.ContentLength = info.Size()
	request.Header.Set("Content-Type", "application/zip")
	response, err := m.webDAVClient().Do(request)
	if err != nil {
		return fmt.Errorf("WebDAV upload failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return webDAVStatusError("WebDAV upload", response)
	}
	return nil
}

func (m Manager) ensureWebDAVDirectories(ctx context.Context, config webDAVStoredConfig) error {
	segments := strings.Split(config.RemotePath, "/")
	for index := range segments {
		remoteURL, err := webDAVURL(config.BaseURL, strings.Join(segments[:index+1], "/"))
		if err != nil {
			return err
		}
		request, err := m.newWebDAVRequest(ctx, config, "MKCOL", remoteURL, nil)
		if err != nil {
			return err
		}
		response, err := m.webDAVClient().Do(request)
		if err != nil {
			return fmt.Errorf("create WebDAV directory: %w", err)
		}
		if response.StatusCode != http.StatusMethodNotAllowed && (response.StatusCode < 200 || response.StatusCode >= 300) {
			err := webDAVStatusError("create WebDAV directory", response)
			response.Body.Close()
			return err
		}
		response.Body.Close()
	}
	return nil
}

func (m Manager) newWebDAVRequest(ctx context.Context, config webDAVStoredConfig, method, target string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if config.Username != "" || config.Password != "" {
		request.SetBasicAuth(config.Username, config.Password)
	}
	request.Header.Set("User-Agent", "PalPanel-WebDAV/1")
	return request, nil
}

func (m Manager) webDAVClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Timeout:   10 * time.Minute,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func webDAVURL(baseURL string, segments ...string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parts := []string{parsed.Path}
	for _, group := range segments {
		for _, segment := range strings.Split(strings.Trim(group, "/"), "/") {
			if segment != "" {
				parts = append(parts, segment)
			}
		}
	}
	parsed.Path = path.Join(parts...)
	return parsed.String(), nil
}

func webDAVStatusError(operation string, response *http.Response) error {
	return fmt.Errorf("%s returned HTTP %d (%s)", operation, response.StatusCode, http.StatusText(response.StatusCode))
}
