package boss

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	rconPacketResponseValue = 0
	rconPacketExecCommand   = 2
	rconPacketAuthResponse  = 2
	rconPacketAuth          = 3
	rconMaxPacketSize       = 1 << 20
)

var bossTemplateFilePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type PalDefenderRCONOptions struct {
	Host           string
	Port           int
	Password       string
	RCONEnabled    *bool
	PalDefenderDir string
	Timeout        time.Duration
}

type PalDefenderRCONExecutor struct {
	host           string
	port           int
	password       string
	rconEnabled    *bool
	palDefenderDir string
	timeout        time.Duration
	dial           func(context.Context, string, string) (net.Conn, error)
}

func NewPalDefenderRCONExecutor(options PalDefenderRCONOptions) *PalDefenderRCONExecutor {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	executor := &PalDefenderRCONExecutor{
		host: strings.TrimSpace(options.Host), port: options.Port,
		password: strings.TrimSpace(options.Password), rconEnabled: options.RCONEnabled, palDefenderDir: strings.TrimSpace(options.PalDefenderDir),
		timeout: timeout,
	}
	executor.dial = (&net.Dialer{Timeout: timeout}).DialContext
	return executor
}

func (e *PalDefenderRCONExecutor) Status(_ context.Context) ExecutionAdapterStatus {
	status := ExecutionAdapterStatus{
		Adapter: "paldefender_rcon_palsummon",
		State:   "ready",
		Capabilities: ExecutionCapabilities{
			FixedCoordinates: true, MultipleSpawns: true, Uncapturable: true,
			CustomPalTemplate: true, ExactMultipliers: false,
		},
		Limitations: []string{
			"生命、攻击和防御倍率不能直接转换为 PalDefender PalTemplate；倍率不为 1 时必须指定 PalTemplate 文件。",
			"命令发出后连接中断时结果会标记为 uncertain，不能自动重试。",
			"本版只执行下一条等待中的波次，不会按延迟自动连续执行。",
		},
	}
	switch {
	case e == nil:
		status.State = "not_configured"
		status.Message = "PalDefender RCON executor is not configured."
	case e.rconEnabled != nil && !*e.rconEnabled:
		status.State = "rcon_disabled"
		status.Message = "RCON is disabled in PalWorldSettings.ini."
	case e.host == "" || e.port < 1 || e.port > 65535:
		status.State = "rcon_not_configured"
		status.Message = "RCON host or port is invalid."
	case e.password == "":
		status.State = "password_missing"
		status.Message = "AdminPassword is empty; RCON authentication cannot be performed."
	case e.palDefenderDir == "":
		status.State = "paldefender_path_missing"
		status.Message = "PalDefender directory is unavailable."
	default:
		templateDir := filepath.Join(e.palDefenderDir, "Pals", "Templates")
		summonDir := filepath.Join(e.palDefenderDir, "Pals", "Summons")
		if err := requireRegularDirectory(e.palDefenderDir); err != nil {
			status.State = "paldefender_path_missing"
			status.Message = err.Error()
		} else if err := ensureRegularDirectory(templateDir); err != nil {
			status.State = "template_directory_unavailable"
			status.Message = err.Error()
		} else if err := ensureRegularDirectory(summonDir); err != nil {
			status.State = "summon_directory_unavailable"
			status.Message = err.Error()
		} else {
			status.Available = true
			status.Message = "PalDefender PalSummon files and RCON /summon are configured."
		}
	}
	return status
}

func (e *PalDefenderRCONExecutor) ExecuteWave(ctx context.Context, summon Summon, wave SummonWave) (WaveExecutionResult, error) {
	result := WaveExecutionResult{
		Adapter: "paldefender_rcon_palsummon", Commands: []string{}, Responses: []string{}, Details: map[string]any{
			"summon_id": summon.ID, "wave_position": wave.Position, "pal_id": wave.PalID,
			"requested_count": wave.Count, "capturable": wave.Capturable,
		},
	}
	status := e.Status(ctx)
	if !status.Available {
		return result, executionFailure(false, "%s", status.Message)
	}
	if strings.TrimSpace(wave.PalID) == "" || wave.Level < 1 || wave.Count < 1 {
		return result, executionFailure(false, "wave contains an invalid PalID, level, or count")
	}

	templateDir := filepath.Join(e.palDefenderDir, "Pals", "Templates")
	summonDir := filepath.Join(e.palDefenderDir, "Pals", "Summons")
	existingTemplate := metadataString(wave.Metadata, "pal_template_file")
	if existingTemplate == "" {
		existingTemplate = metadataString(summon.Metadata, "pal_template_file")
	}
	templateFile := existingTemplate
	generatedTemplate := ""
	if templateFile != "" {
		var err error
		templateFile, err = normalizeBossTemplateFile(templateFile)
		if err != nil {
			return result, executionFailure(false, "invalid PalTemplate filename: %v", err)
		}
		templateDetails, err := inspectBossTemplateFile(filepath.Join(templateDir, templateFile), wave.PalID, wave.Level)
		if err != nil {
			return result, executionFailure(false, "PalTemplate %s is unavailable or incompatible: %v", templateFile, err)
		}
		result.Details["pal_template_source"] = "existing"
		result.Details["pal_template"] = templateDetails
	} else {
		if !approximatelyOne(wave.HPMultiplier) || !approximatelyOne(wave.AttackMultiplier) || !approximatelyOne(wave.DefenseMultiplier) {
			return result, executionFailure(false, "wave uses stat multipliers %.2fx/%.2fx/%.2fx but no pal_template_file was configured; execution was blocked to avoid spawning the wrong Boss", wave.HPMultiplier, wave.AttackMultiplier, wave.DefenseMultiplier)
		}
		base := generatedBossFileBase(summon.ID, wave.Position)
		templateFile = base + "_template.json"
		generatedTemplate = filepath.Join(templateDir, templateFile)
		document := map[string]any{
			"PalID":             wave.PalID,
			"Nickname":          truncateRunes(strings.TrimSpace(wave.Name), 64),
			"Level":             wave.Level,
			"PartnerSkillLevel": 1,
		}
		if err := writeAtomicJSON(generatedTemplate, document); err != nil {
			return result, executionFailure(false, "write generated PalTemplate: %v", err)
		}
		defer os.Remove(generatedTemplate)
		result.Details["pal_template_source"] = "generated_minimal"
	}
	result.Details["pal_template_file"] = templateFile

	disableStatuses := metadataStringSlice(wave.Metadata, "disable_statuses")
	if len(disableStatuses) == 0 {
		disableStatuses = metadataStringSlice(summon.Metadata, "disable_statuses")
	}
	result.Details["disable_statuses"] = disableStatuses

	for index := 0; index < wave.Count; index++ {
		if err := ctx.Err(); err != nil {
			return result, executionFailure(result.CompletedCommands > 0, "execution context ended: %v", err)
		}
		x, y := spreadCoordinate(summon.Location.X, summon.Location.Y, wave.SpawnRadius, wave.Count, index)
		base := fmt.Sprintf("%s_%02d", generatedBossFileBase(summon.ID, wave.Position), index+1)
		summonFile := base + ".json"
		summonPath := filepath.Join(summonDir, summonFile)
		document := map[string]any{
			"PalTemplate":  templateFile,
			"Uncapturable": !wave.Capturable,
			"X":            x,
			"Y":            y,
			"Z":            summon.Location.Z,
		}
		if len(disableStatuses) > 0 {
			document["DisableStatuses"] = disableStatuses
		}
		if err := writeAtomicJSON(summonPath, document); err != nil {
			return result, executionFailure(result.CompletedCommands > 0, "write PalSummon file %s: %v", summonFile, err)
		}
		command := "/summon " + strings.TrimSuffix(summonFile, ".json")
		result.Commands = append(result.Commands, command)
		response, commandSent, err := e.executeRCON(ctx, command)
		_ = os.Remove(summonPath)
		if response != "" {
			result.Responses = append(result.Responses, response)
		} else {
			result.Responses = append(result.Responses, "")
		}
		if err != nil {
			uncertain := commandSent || result.CompletedCommands > 0
			return result, executionFailure(uncertain, "RCON command %d/%d failed: %v", index+1, wave.Count, err)
		}
		if responseIndicatesFailure(response) {
			return result, executionFailure(result.CompletedCommands > 0, "PalDefender rejected command %d/%d: %s", index+1, wave.Count, response)
		}
		result.CompletedCommands++
	}
	result.Details["completed_count"] = result.CompletedCommands
	return result, nil
}

func (e *PalDefenderRCONExecutor) executeRCON(ctx context.Context, command string) (string, bool, error) {
	address := net.JoinHostPort(e.host, fmt.Sprintf("%d", e.port))
	dial := e.dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: e.timeout}).DialContext
	}
	conn, err := dial(ctx, "tcp", address)
	if err != nil {
		return "", false, fmt.Errorf("connect %s: %w", address, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(e.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return "", false, err
	}
	reader := bufio.NewReaderSize(conn, 4096)
	if err := writeRCONPacket(conn, 1, rconPacketAuth, e.password); err != nil {
		return "", false, fmt.Errorf("write RCON authentication: %w", err)
	}
	authenticated := false
	for reads := 0; reads < 4; reads++ {
		packet, readErr := readRCONPacket(reader)
		if readErr != nil {
			return "", false, fmt.Errorf("read RCON authentication: %w", readErr)
		}
		if packet.typ == rconPacketAuthResponse {
			if packet.id == -1 {
				return "", false, errors.New("RCON authentication failed")
			}
			authenticated = true
			break
		}
	}
	if !authenticated {
		return "", false, errors.New("RCON authentication response was not received")
	}
	if err := writeRCONPacket(conn, 2, rconPacketExecCommand, command); err != nil {
		return "", false, fmt.Errorf("write RCON command: %w", err)
	}
	commandSent := true
	var response bytes.Buffer
	receivedPacket := false
	for reads := 0; reads < 64; reads++ {
		packet, readErr := readRCONPacket(reader)
		if readErr != nil {
			if timeout, ok := readErr.(net.Error); ok && timeout.Timeout() && receivedPacket {
				break
			}
			return strings.TrimSpace(response.String()), commandSent, fmt.Errorf("read RCON response: %w", readErr)
		}
		if packet.id != 2 {
			continue
		}
		receivedPacket = true
		response.WriteString(packet.body)
		if packet.typ == rconPacketResponseValue {
			_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		}
	}
	return strings.TrimSpace(response.String()), commandSent, nil
}

type rconPacket struct {
	id   int32
	typ  int32
	body string
}

func writeRCONPacket(writer io.Writer, id, typ int32, body string) error {
	payload := []byte(body)
	size := int32(4 + 4 + len(payload) + 2)
	if size < 10 || size > rconMaxPacketSize {
		return errors.New("RCON packet size is invalid")
	}
	buffer := bytes.NewBuffer(make([]byte, 0, int(size)+4))
	for _, value := range []int32{size, id, typ} {
		if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
			return err
		}
	}
	buffer.Write(payload)
	buffer.WriteByte(0)
	buffer.WriteByte(0)
	payloadBytes := buffer.Bytes()
	for len(payloadBytes) > 0 {
		written, err := writer.Write(payloadBytes)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		payloadBytes = payloadBytes[written:]
	}
	return nil
}

func readRCONPacket(reader io.Reader) (rconPacket, error) {
	var size int32
	if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
		return rconPacket{}, err
	}
	if size < 10 || size > rconMaxPacketSize {
		return rconPacket{}, fmt.Errorf("RCON response packet size %d is invalid", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return rconPacket{}, err
	}
	packet := rconPacket{
		id:  int32(binary.LittleEndian.Uint32(payload[0:4])),
		typ: int32(binary.LittleEndian.Uint32(payload[4:8])),
	}
	body := payload[8:]
	if len(body) < 2 || body[len(body)-1] != 0 || body[len(body)-2] != 0 {
		return rconPacket{}, errors.New("RCON response terminator is invalid")
	}
	packet.body = string(body[:len(body)-2])
	return packet, nil
}

func requireRegularDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s is not a regular directory", path)
	}
	return nil
}

func ensureRegularDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s is not a regular directory", path)
	}
	return nil
}

func inspectBossTemplateFile(path, expectedPalID string, expectedLevel int) (map[string]any, error) {
	if err := requireRegularFile(path); err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document struct {
		PalID string `json:"PalID"`
		Level int    `json:"Level"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	document.PalID = strings.TrimSpace(document.PalID)
	if document.PalID == "" {
		return nil, errors.New("PalID is missing")
	}
	if !strings.EqualFold(document.PalID, strings.TrimSpace(expectedPalID)) {
		return nil, fmt.Errorf("PalID %q does not match wave PalID %q", document.PalID, expectedPalID)
	}
	if document.Level > 0 && expectedLevel > 0 && document.Level != expectedLevel {
		return nil, fmt.Errorf("Level %d does not match wave level %d", document.Level, expectedLevel)
	}
	return map[string]any{"pal_id": document.PalID, "level": document.Level}, nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("path is not a regular file")
	}
	if info.Size() > 4<<20 {
		return errors.New("file exceeds 4 MiB")
	}
	return nil
}

func writeAtomicJSON(path string, value any) error {
	if err := ensureRegularDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".palpanel-boss-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	complete := false
	defer func() {
		_ = temporary.Close()
		if !complete {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	_ = os.Remove(path)
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	complete = true
	return nil
}

func normalizeBossTemplateFile(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return "", errors.New("filename must not contain a path")
	}
	if !strings.EqualFold(filepath.Ext(value), ".json") {
		value += ".json"
	}
	if !bossTemplateFilePattern.MatchString(value) {
		return "", errors.New("filename contains unsupported characters")
	}
	return value, nil
}

func generatedBossFileBase(summonID string, position int) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, summonID)
	clean = strings.Trim(clean, "_")
	if len(clean) > 48 {
		clean = clean[:48]
	}
	if clean == "" {
		clean = "summon"
	}
	return fmt.Sprintf("PalPanelBoss_%s_W%02d", clean, position)
}

func spreadCoordinate(x, y, radius float64, count, index int) (float64, float64) {
	if count <= 1 || radius <= 0 {
		return x, y
	}
	angle := 2 * math.Pi * float64(index) / float64(count)
	return x + math.Cos(angle)*radius, y + math.Sin(angle)*radius
}

func approximatelyOne(value float64) bool {
	return math.Abs(value-1) < 0.000001
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	value, found := metadata[key]
	if !found {
		return nil
	}
	result := []string{}
	seen := map[string]bool{}
	appendValue := func(item string) {
		item = strings.TrimSpace(item)
		if item == "" || len(item) > 64 || seen[item] {
			return
		}
		seen[item] = true
		result = append(result, item)
	}
	switch typed := value.(type) {
	case []string:
		for _, item := range typed {
			appendValue(item)
		}
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok {
				appendValue(text)
			}
		}
	}
	if len(result) > 32 {
		result = result[:32]
	}
	return result
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

func responseIndicatesFailure(response string) bool {
	trimmed := strings.TrimSpace(response)
	if trimmed == "" {
		return false
	}
	var payload map[string]any
	if json.Unmarshal([]byte(trimmed), &payload) == nil && payload != nil {
		if success, ok := payload["success"].(bool); ok && !success {
			return true
		}
		for _, key := range []string{"error", "failure"} {
			if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
				return true
			}
		}
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{"command failed", "unknown command", "invalid command", "not found", "exception", "permission denied", "access denied"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
