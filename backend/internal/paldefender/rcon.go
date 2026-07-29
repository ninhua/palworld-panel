package paldefender

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/palconfig"
)

const (
	rconPacketAuth          int32 = 3
	rconPacketExec          int32 = 2
	rconPacketAuthReply     int32 = 2
	rconPacketExecReply     int32 = 0
	rconPacketLimit               = 4 << 20
	rconResponseLimit             = 8 << 20
	rconTimeout                   = 5 * time.Second
	rconResponseIdleTimeout       = 150 * time.Millisecond
	rconCatalogTimeout            = 30 * time.Second
	rconCatalogIdleTimeout        = time.Second
)

var (
	ErrRCONDisabled          = errors.New("Palworld RCON is disabled")
	ErrRCONPasswordMissing   = errors.New("Palworld administrator password is missing")
	ErrRCONUnavailable       = errors.New("Palworld RCON is unavailable")
	ErrRCONAuthentication    = errors.New("Palworld RCON authentication failed")
	ErrRCONInvalidResponse   = errors.New("Palworld RCON returned an invalid response")
	ErrRCONCommandRejected   = errors.New("PalDefender rejected the RCON command")
	ErrRCONBase64Unsupported = errors.New("PalDefender RCONbase64 must be disabled for panel-managed commands")
	whitelistUserPattern     = regexp.MustCompile(`(?i)(?:steam|gdk|ps5)_[A-Za-z0-9_-]+`)
	adminIPPattern           = regexp.MustCompile(`^(?:\d{1,3}|\*)(?:\.(?:\d{1,3}|\*)){3}$`)
)

type RCONResult struct {
	Command string   `json:"command"`
	Output  string   `json:"output"`
	Entries []string `json:"entries,omitempty"`
}

type CommandCatalogEntry struct {
	Name         string `json:"name"`
	Syntax       string `json:"syntax"`
	Description  string `json:"description"`
	Category     string `json:"category"`
	Transport    string `json:"transport"`
	Destructive  bool   `json:"destructive"`
	ReferenceURL string `json:"reference_url"`
}

type rconPacket struct {
	ID   int32
	Type int32
	Body string
}

type rconExecutionOptions struct {
	firstResponseTimeout time.Duration
	responseIdleTimeout  time.Duration
}

var defaultRCONExecutionOptions = rconExecutionOptions{
	firstResponseTimeout: rconTimeout,
	responseIdleTimeout:  rconResponseIdleTimeout,
}

func (m Manager) RCONWhitelist(ctx context.Context) (RCONResult, error) {
	return m.runTypedRCON(ctx, "/whitelist_get", true)
}

func (m Manager) RCONWhitelistAdd(ctx context.Context, identifier string) (RCONResult, error) {
	identifier, err := validatePlayerIdentifier(identifier)
	if err != nil {
		return RCONResult{}, err
	}
	return m.runTypedRCON(ctx, "/whitelist_add "+identifier, true)
}

func (m Manager) RCONWhitelistRemove(ctx context.Context, identifier string) (RCONResult, error) {
	identifier, err := validatePlayerIdentifier(identifier)
	if err != nil {
		return RCONResult{}, err
	}
	return m.runTypedRCON(ctx, "/whitelist_remove "+identifier, true)
}

func (m Manager) RCONSetAdmin(ctx context.Context, identifier string) (RCONResult, error) {
	identifier, err := validatePlayerIdentifier(identifier)
	if err != nil {
		return RCONResult{}, err
	}
	return m.runTypedRCON(ctx, "/setadmin "+identifier, false)
}

func (m Manager) RCONExportPals(ctx context.Context, identifier string) (RCONResult, error) {
	identifier, err := validatePlayerIdentifier(identifier)
	if err != nil {
		return RCONResult{}, err
	}
	return m.runTypedRCON(ctx, "/exportpals "+identifier, false)
}

func (m Manager) RCONTechnologyCatalog(ctx context.Context) (RCONResult, error) {
	return m.runTypedRCON(ctx, "/gettechids", true)
}

func (m Manager) RCONSkinCatalog(ctx context.Context) (RCONResult, error) {
	return m.runTypedRCON(ctx, "/getskinids", true)
}

func (m Manager) RCONCommands(ctx context.Context) (RCONResult, error) {
	return m.runTypedRCONWithOptions(ctx, "/getrconcmds", false, rconExecutionOptions{
		firstResponseTimeout: rconCatalogTimeout,
		responseIdleTimeout:  rconCatalogIdleTimeout,
	})
}

func (m Manager) runTypedRCON(ctx context.Context, command string, parseEntries bool) (RCONResult, error) {
	return m.runTypedRCONWithOptions(ctx, command, parseEntries, defaultRCONExecutionOptions)
}

func (m Manager) runTypedRCONWithOptions(ctx context.Context, command string, parseEntries bool, options rconExecutionOptions) (RCONResult, error) {
	output, err := m.executeRCONWithOptions(ctx, command, options)
	if err != nil {
		return RCONResult{}, err
	}
	if isUnknownRCONCommand(output) && strings.HasPrefix(command, "/") {
		compatibleCommand := strings.TrimPrefix(command, "/")
		compatibleOutput, compatibleErr := m.executeRCONWithOptions(ctx, compatibleCommand, options)
		if compatibleErr != nil {
			return RCONResult{}, compatibleErr
		}
		command, output = compatibleCommand, compatibleOutput
	}
	if isRejectedRCONOutput(output) {
		return RCONResult{}, fmt.Errorf("%w: %s", ErrRCONCommandRejected, strings.TrimSpace(output))
	}
	result := RCONResult{Command: command, Output: output}
	if parseEntries {
		result.Entries = parseRCONEntries(output)
	}
	return result, nil
}

func isUnknownRCONCommand(output string) bool {
	normalized := strings.ToLower(strings.TrimSpace(output))
	return normalized == "unknown command" ||
		strings.HasPrefix(normalized, "unknown command:") ||
		strings.Contains(normalized, "command not found")
}

func isRejectedRCONOutput(output string) bool {
	normalized := strings.ToLower(strings.TrimSpace(output))
	return isUnknownRCONCommand(output) ||
		strings.HasPrefix(normalized, "invalid command") ||
		strings.HasPrefix(normalized, "error: unknown command")
}

func (m Manager) executeRCON(ctx context.Context, command string) (string, error) {
	return m.executeRCONWithOptions(ctx, command, defaultRCONExecutionOptions)
}

func (m Manager) executeRCONWithOptions(ctx context.Context, command string, options rconExecutionOptions) (string, error) {
	if err := m.validateRCONPrerequisites(ctx); err != nil {
		return "", err
	}
	if options.firstResponseTimeout <= 0 {
		options.firstResponseTimeout = rconTimeout
	}
	if options.responseIdleTimeout <= 0 {
		options.responseIdleTimeout = rconResponseIdleTimeout
	}
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 4096 || strings.ContainsAny(command, "\r\n\x00") {
		return "", invalidRESTRequest("invalid RCON command")
	}
	settings, err := palconfig.Read(m.cfg.PalWorldSettingsPath())
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(strings.TrimSpace(settings["RCONEnabled"]), "True") {
		return "", ErrRCONDisabled
	}
	password := strings.TrimSpace(settings["AdminPassword"])
	if password == "" {
		return "", ErrRCONPasswordMissing
	}
	config, err := m.ReadConfig()
	if err == nil {
		if base64Enabled, ok := config["RCONbase64"].(bool); ok && base64Enabled {
			return "", ErrRCONBase64Unsupported
		}
	}
	port := strings.TrimSpace(settings["RCONPort"])
	if port == "" {
		port = strconv.Itoa(m.cfg.EffectiveRCONPort())
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("%w: invalid RCON port", ErrRCONUnavailable)
	}
	host := m.cfg.EffectiveRCONHost()
	if m.store != nil {
		if runtimeMode, found, readErr := m.store.GetKV(ctx, kvRuntimeMode); readErr == nil && found && runtimeMode == runtimeWineDocker && rconLoopbackHost(host) {
			if portNumber != m.cfg.EffectiveRCONPort() {
				return "", fmt.Errorf("%w: PalWorldSettings.ini RCONPort %d does not match Linux container mapping %d", ErrRCONUnavailable, portNumber, m.cfg.EffectiveRCONPort())
			}
			portNumber = m.cfg.EffectiveRCONPort()
		}
	}
	target := net.JoinHostPort(host, strconv.Itoa(portNumber))
	dialer := net.Dialer{Timeout: rconTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		if m.cfg.DebugLogger != nil {
			m.cfg.DebugLogger.Printf("paldefender rcon endpoint=%s command=%q state=failed error=%q", target, command, err.Error())
		}
		return "", fmt.Errorf("%w: %v", ErrRCONUnavailable, err)
	}
	if m.cfg.DebugLogger != nil {
		m.cfg.DebugLogger.Printf("paldefender rcon endpoint=%s command=%q state=connected", target, command)
	}
	defer conn.Close()
	deadline := time.Now().Add(rconTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	if err := writeRCONPacket(conn, rconPacket{ID: 1, Type: rconPacketAuth, Body: password}); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRCONUnavailable, err)
	}
	authenticated := false
	for attempts := 0; attempts < 3; attempts++ {
		packet, err := readRCONPacket(conn)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrRCONInvalidResponse, err)
		}
		if packet.ID == -1 {
			return "", ErrRCONAuthentication
		}
		if packet.ID == 1 && packet.Type == rconPacketAuthReply {
			authenticated = true
			break
		}
	}
	if !authenticated {
		return "", ErrRCONAuthentication
	}
	if err := writeRCONPacket(conn, rconPacket{ID: 2, Type: rconPacketExec, Body: command}); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRCONUnavailable, err)
	}
	responseDeadline := time.Now().Add(options.firstResponseTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(responseDeadline) {
		responseDeadline = contextDeadline
	}
	_ = conn.SetReadDeadline(responseDeadline)
	var response strings.Builder
	received := false
	for {
		packet, err := readRCONPacket(conn)
		if err != nil {
			var netErr net.Error
			if received && errors.As(err, &netErr) && netErr.Timeout() {
				break
			}
			return "", fmt.Errorf("%w: %v", ErrRCONInvalidResponse, err)
		}
		if packet.ID == -1 {
			return "", ErrRCONAuthentication
		}
		if packet.ID != 2 || packet.Type != rconPacketExecReply {
			continue
		}
		received = true
		if response.Len()+len(packet.Body) > rconResponseLimit {
			return "", ErrRCONInvalidResponse
		}
		response.WriteString(packet.Body)
		responseDeadline = time.Now().Add(options.responseIdleTimeout)
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(responseDeadline) {
			responseDeadline = contextDeadline
		}
		_ = conn.SetReadDeadline(responseDeadline)
	}
	if !received {
		return "", ErrRCONInvalidResponse
	}
	return strings.TrimSpace(strings.ReplaceAll(response.String(), "\x00", "")), nil
}

func rconLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (m Manager) validateRCONPrerequisites(ctx context.Context) error {
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		return ErrPalDefenderNotInstalled
	}
	if !status.LoadVerified {
		return ErrPalDefenderNotLoaded
	}
	return nil
}

func writeRCONPacket(writer io.Writer, packet rconPacket) error {
	body := []byte(packet.Body)
	size := int32(len(body) + 10)
	buffer := bytes.NewBuffer(make([]byte, 0, size+4))
	for _, value := range []int32{size, packet.ID, packet.Type} {
		if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
			return err
		}
	}
	buffer.Write(body)
	buffer.WriteByte(0)
	buffer.WriteByte(0)
	_, err := writer.Write(buffer.Bytes())
	return err
}

func readRCONPacket(reader io.Reader) (rconPacket, error) {
	var size int32
	if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
		return rconPacket{}, err
	}
	if size < 10 || size > rconPacketLimit {
		return rconPacket{}, fmt.Errorf("invalid packet size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return rconPacket{}, err
	}
	id := int32(binary.LittleEndian.Uint32(payload[0:4]))
	packetType := int32(binary.LittleEndian.Uint32(payload[4:8]))
	if payload[len(payload)-2] != 0 || payload[len(payload)-1] != 0 {
		return rconPacket{}, errors.New("packet terminator was invalid")
	}
	return rconPacket{ID: id, Type: packetType, Body: string(payload[8 : len(payload)-2])}, nil
}

func parseRCONEntries(output string) []string {
	var raw any
	if json.Unmarshal([]byte(output), &raw) == nil {
		var values []string
		collectJSONStrings(raw, &values)
		if len(values) > 0 {
			return uniqueSortedStrings(values)
		}
	}
	return uniqueSortedStrings(whitelistUserPattern.FindAllString(output, -1))
}

func collectJSONStrings(value any, out *[]string) {
	switch item := value.(type) {
	case string:
		item = strings.TrimSpace(item)
		if item != "" {
			*out = append(*out, item)
		}
	case []any:
		for _, child := range item {
			collectJSONStrings(child, out)
		}
	case map[string]any:
		for _, child := range item {
			collectJSONStrings(child, out)
		}
	}
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[strings.ToLower(value)] = value
		}
	}
	out := make([]string, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func validateAdminIP(value string) (string, error) {
	value = strings.TrimSpace(value)
	if ip := net.ParseIP(value); ip != nil {
		return value, nil
	}
	if !adminIPPattern.MatchString(value) {
		return "", invalidRESTRequest("invalid administrator IP or wildcard")
	}
	for _, segment := range strings.Split(value, ".") {
		if segment == "*" {
			continue
		}
		number, err := strconv.Atoi(segment)
		if err != nil || number < 0 || number > 255 {
			return "", invalidRESTRequest("invalid administrator IP or wildcard")
		}
	}
	return value, nil
}

func PalDefenderCommandCatalog() []CommandCatalogEntry {
	const source = "https://github.com/Ultimeit/PalDefender/blob/main/docs/zh/Commands/index.md"
	return []CommandCatalogEntry{
		{Name: "whitelist_get", Syntax: "/whitelist_get", Description: "查看 PalDefender 白名单", Category: "access", Transport: "rcon", ReferenceURL: source},
		{Name: "whitelist_add", Syntax: "/whitelist_add <UserId>", Description: "添加白名单玩家", Category: "access", Transport: "rcon", ReferenceURL: source},
		{Name: "whitelist_remove", Syntax: "/whitelist_remove <UserId>", Description: "移除白名单玩家", Category: "access", Transport: "rcon", ReferenceURL: source},
		{Name: "setadmin", Syntax: "/setadmin <UserId>", Description: "临时授予或撤销管理员权限", Category: "access", Transport: "rcon", ReferenceURL: source},
		{Name: "exportpals", Syntax: "/exportpals <UserId>", Description: "把玩家帕鲁导出为 PalTemplate 文件", Category: "pals", Transport: "rcon", ReferenceURL: source},
		{Name: "learntech", Syntax: "/learntech <UserId> <TechID>", Description: "解锁指定或全部科技", Category: "technology", Transport: "rest", ReferenceURL: source},
		{Name: "unlearntech", Syntax: "/unlearntech <UserId> <TechID>", Description: "遗忘指定或全部科技", Category: "technology", Transport: "rest", Destructive: true, ReferenceURL: source},
		{Name: "givetechpoints", Syntax: "/givetechpoints <UserId> [Amount]", Description: "发放科技点", Category: "progression", Transport: "rest", ReferenceURL: source},
		{Name: "givebosstechpoints", Syntax: "/givebosstechpoints <UserId> [Amount]", Description: "发放古代科技点", Category: "progression", Transport: "rest", ReferenceURL: source},
		{Name: "givepal", Syntax: "/givepal <UserId> <PalId> [Level]", Description: "按 ID 和等级发放帕鲁", Category: "pals", Transport: "rest", ReferenceURL: source},
		{Name: "givepal_j", Syntax: "/givepal_j <UserId> <PalTemplate>", Description: "按模板发放带属性的帕鲁", Category: "pals", Transport: "rest", ReferenceURL: source},
		{Name: "delitems", Syntax: "/delitems <UserId> <ItemId>[:<Amount>] ...", Description: "从玩家背包移除一种或多种物品", Category: "items", Transport: "rcon", Destructive: true, ReferenceURL: source},
		{Name: "tp", Syntax: "/tp <UserId> <X> <Y> [Z] 或 /tp <UserId1> <UserId2>", Description: "把在线玩家传送到坐标或另一名玩家", Category: "player", Transport: "rcon", ReferenceURL: source},
		{Name: "deletepals", Syntax: "/deletepals <UserId> <PalFilter>", Description: "按结构化筛选条件删除玩家帕鲁", Category: "pals", Transport: "rcon", Destructive: true, ReferenceURL: source},
		{Name: "gettechids", Syntax: "/gettechids", Description: "读取当前服务端可用科技 ID", Category: "catalog", Transport: "rcon", ReferenceURL: source},
		{Name: "getskinids", Syntax: "/getskinids", Description: "读取当前服务端可用皮肤 ID", Category: "catalog", Transport: "rcon", ReferenceURL: source},
	}
}
