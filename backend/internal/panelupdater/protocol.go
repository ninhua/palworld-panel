package panelupdater

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const SchemaVersion = 1

var (
	versionPattern      = regexp.MustCompile(`^v\d+\.\d+\.\d+-custom\.\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?$`)
	versionPartsPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-custom\.(\d+)\.(\d+)\.(\d+)(?:[-+][A-Za-z0-9.-]+)?$`)
	sha256Pattern       = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	jobIDPattern        = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

type Request struct {
	SchemaVersion  int    `json:"schema_version"`
	JobID          string `json:"job_id"`
	ArchivePath    string `json:"archive_path"`
	ArchiveSHA256  string `json:"archive_sha256"`
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	HealthPort     int    `json:"health_port"`
	ProxyURL       string `json:"proxy_url,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type Result struct {
	SchemaVersion     int    `json:"schema_version"`
	JobID             string `json:"job_id"`
	CurrentVersion    string `json:"current_version"`
	TargetVersion     string `json:"target_version"`
	Status            string `json:"status"`
	Message           string `json:"message"`
	ErrorCode         string `json:"error_code,omitempty"`
	Detail            string `json:"detail,omitempty"`
	RollbackAttempted bool   `json:"rollback_attempted"`
	RollbackSucceeded bool   `json:"rollback_succeeded"`
	FinishedAt        string `json:"finished_at"`
}

func StateDir(dataDir string) string {
	return filepath.Join(filepath.Clean(dataDir), "panel-update")
}

func RequestPath(dataDir string) string {
	return filepath.Join(StateDir(dataDir), "request.json")
}

func InProgressPath(dataDir string) string {
	return filepath.Join(StateDir(dataDir), "request.in-progress.json")
}

func ResultPath(dataDir string) string {
	return filepath.Join(StateDir(dataDir), "result.json")
}

func OperationsDir(dataDir string) string {
	return filepath.Join(StateDir(dataDir), "operations")
}

func OperationDir(dataDir, jobID string) string {
	return filepath.Join(OperationsDir(dataDir), jobID)
}

func (request Request) Validate(stateDir string) error {
	if request.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported update request schema: %d", request.SchemaVersion)
	}
	if !jobIDPattern.MatchString(request.JobID) {
		return errors.New("invalid update job id")
	}
	if !versionPattern.MatchString(request.CurrentVersion) {
		return fmt.Errorf("invalid current version: %s", request.CurrentVersion)
	}
	if !versionPattern.MatchString(request.TargetVersion) {
		return fmt.Errorf("invalid target version: %s", request.TargetVersion)
	}
	comparison, err := CompareVersions(request.CurrentVersion, request.TargetVersion)
	if err != nil {
		return err
	}
	if comparison >= 0 {
		return errors.New("target version must be newer than current version")
	}
	if !sha256Pattern.MatchString(request.ArchiveSHA256) {
		return errors.New("invalid archive sha256")
	}
	if request.HealthPort < 1 || request.HealthPort > 65535 {
		return errors.New("invalid panel health port")
	}
	if strings.TrimSpace(request.ProxyURL) != "" {
		parsed, err := url.Parse(request.ProxyURL)
		if err != nil || parsed.Host == "" || parsed.Opaque != "" {
			return errors.New("invalid update proxy URL")
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return errors.New("invalid update proxy URL")
		}
		if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("invalid update proxy URL")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, request.CreatedAt); err != nil {
		return errors.New("invalid request creation time")
	}
	archive, err := filepath.Abs(filepath.Clean(request.ArchivePath))
	if err != nil {
		return fmt.Errorf("resolve archive path: %w", err)
	}
	root, err := filepath.Abs(filepath.Clean(stateDir))
	if err != nil {
		return fmt.Errorf("resolve update state directory: %w", err)
	}
	relative, err := filepath.Rel(root, archive)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("archive path is outside update state directory")
	}
	return nil
}

func CompareVersions(current, target string) (int, error) {
	currentParts, err := numericVersionParts(current)
	if err != nil {
		return 0, err
	}
	targetParts, err := numericVersionParts(target)
	if err != nil {
		return 0, err
	}
	for index := range currentParts {
		if currentParts[index] < targetParts[index] {
			return -1, nil
		}
		if currentParts[index] > targetParts[index] {
			return 1, nil
		}
	}
	return 0, nil
}

func numericVersionParts(version string) ([6]uint64, error) {
	var parts [6]uint64
	matches := versionPartsPattern.FindStringSubmatch(strings.TrimSpace(version))
	if len(matches) != len(parts)+1 {
		return parts, fmt.Errorf("invalid panel version: %s", version)
	}
	for index := range parts {
		value, err := strconv.ParseUint(matches[index+1], 10, 64)
		if err != nil {
			return parts, fmt.Errorf("invalid panel version: %s", version)
		}
		parts[index] = value
	}
	return parts, nil
}

func WriteRequest(dataDir string, request Request) error {
	stateDir := StateDir(dataDir)
	if err := request.Validate(stateDir); err != nil {
		return err
	}
	if err := os.MkdirAll(OperationsDir(dataDir), 0o700); err != nil {
		return err
	}
	for _, path := range []string{RequestPath(dataDir), InProgressPath(dataDir)} {
		if _, err := os.Lstat(path); err == nil {
			return errors.New("another panel update is already pending")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	body, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}
	return writeJSONAtomic(RequestPath(dataDir), append(body, '\n'))
}

func ReadRequest(path, stateDir string) (Request, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Request{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Request{}, errors.New("update request is not a regular file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Request{}, err
	}
	if len(body) > 64<<10 {
		return Request{}, errors.New("update request is too large")
	}
	var request Request
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("decode update request: %w", err)
	}
	if err := request.Validate(stateDir); err != nil {
		return Request{}, err
	}
	return request, nil
}

func WriteResult(dataDir string, result Result) error {
	result.SchemaVersion = SchemaVersion
	if result.FinishedAt == "" {
		result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := os.MkdirAll(StateDir(dataDir), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return writeJSONAtomic(ResultPath(dataDir), append(body, '\n'))
}

func writeJSONAtomic(path string, body []byte) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".panel-update-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func PeekResult(dataDir string) (Result, bool, error) {
	path := ResultPath(dataDir)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Result{}, false, errors.New("panel update result is not a regular file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Result{}, false, err
	}
	if len(body) > 64<<10 {
		return Result{}, false, errors.New("panel update result is too large")
	}
	var result Result
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Result{}, false, err
	}
	if result.SchemaVersion != SchemaVersion || !jobIDPattern.MatchString(result.JobID) {
		return Result{}, false, errors.New("invalid panel update result")
	}
	return result, true, nil
}

func ConsumeResult(dataDir string) (Result, bool, error) {
	path := ResultPath(dataDir)
	claim := path + ".consume"
	if err := os.Rename(path, claim); err != nil {
		if os.IsNotExist(err) {
			return Result{}, false, nil
		}
		return Result{}, false, err
	}
	defer os.Remove(claim)
	body, err := os.ReadFile(claim)
	if err != nil {
		return Result{}, false, err
	}
	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		return Result{}, false, err
	}
	if result.SchemaVersion != SchemaVersion || !jobIDPattern.MatchString(result.JobID) {
		return Result{}, false, errors.New("invalid panel update result")
	}
	_ = os.Remove(InProgressPath(dataDir))
	return result, true, nil
}

func ParseListenPort(listen string) (int, error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return 8080, nil
	}
	_, portText, err := net.SplitHostPort(listen)
	if err != nil {
		if strings.Count(listen, ":") == 1 {
			portText = strings.TrimPrefix(listen[strings.LastIndex(listen, ":"):], ":")
		} else {
			return 0, err
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("invalid listen port")
	}
	return port, nil
}
