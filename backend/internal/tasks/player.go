package tasks

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const playerTaskPageSize = 5

type PlayerProgressPage struct {
	PlayerUID string     `json:"player_uid"`
	Query     string     `json:"query,omitempty"`
	Items     []Progress `json:"items"`
	Count     int        `json:"count"`
	Total     int        `json:"total"`
	Limit     int        `json:"limit"`
	Offset    int        `json:"offset"`
}

type PlayerCommandRequest struct {
	PlayerUID        string `json:"player_uid"`
	Message          string `json:"message"`
	CommandPrefix    string `json:"command_prefix,omitempty"`
	AllowBareCommand bool   `json:"allow_bare_command"`
}

type PlayerCommandResult struct {
	PlayerUID string `json:"player_uid,omitempty"`
	Handled   bool   `json:"handled"`
	Command   string `json:"command,omitempty"`
	Reply     string `json:"reply,omitempty"`
	Page      int    `json:"page,omitempty"`
	Query     string `json:"query,omitempty"`
	Count     int    `json:"count,omitempty"`
}

func (s *Service) PlayerProgressPage(ctx context.Context, playerUID, query string, limit, offset int) (PlayerProgressPage, error) {
	playerUID = strings.TrimSpace(playerUID)
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = playerTaskPageSize
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	items, err := s.PlayerProgress(ctx, playerUID)
	if err != nil {
		return PlayerProgressPage{}, err
	}
	filtered := make([]Progress, 0, len(items))
	needle := strings.ToLower(query)
	for _, item := range items {
		if needle != "" && !strings.Contains(strings.ToLower(strings.Join([]string{item.TaskName, item.Description, item.EventType}, " ")), needle) {
			continue
		}
		filtered = append(filtered, item)
	}
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return PlayerProgressPage{PlayerUID: playerUID, Query: query, Items: filtered[offset:end], Count: end - offset, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Service) ExecutePlayerCommand(ctx context.Context, request PlayerCommandRequest) (PlayerCommandResult, error) {
	request.PlayerUID = strings.TrimSpace(request.PlayerUID)
	command, arguments, handled := parsePlayerTaskCommand(request.Message, request.CommandPrefix, request.AllowBareCommand)
	result := PlayerCommandResult{PlayerUID: request.PlayerUID, Handled: handled, Command: command}
	if !handled {
		return result, nil
	}
	if request.PlayerUID == "" {
		result.Reply = "无法识别玩家身份，暂时不能查询任务。"
		return result, nil
	}
	page, query := parsePlayerTaskArguments(arguments)
	progress, err := s.PlayerProgressPage(ctx, request.PlayerUID, query, playerTaskPageSize, (page-1)*playerTaskPageSize)
	if err != nil {
		return PlayerCommandResult{}, err
	}
	result.Page = page
	result.Query = query
	result.Count = progress.Count
	result.Reply = formatPlayerTaskProgress(progress, page)
	return result, nil
}

func parsePlayerTaskCommand(message, prefix string, allowBare bool) (string, string, bool) {
	message = strings.TrimSpace(message)
	prefix = strings.TrimSpace(prefix)
	if message == "" {
		return "", "", false
	}
	if prefix != "" && strings.HasPrefix(message, prefix) {
		message = strings.TrimSpace(strings.TrimPrefix(message, prefix))
	} else if prefix != "" && !allowBare {
		return "", "", false
	}
	for _, label := range []string{"任务进度", "我的任务", "任务"} {
		if message == label {
			return "progress", "", true
		}
		if strings.HasPrefix(message, label) {
			remainder := strings.TrimPrefix(message, label)
			if remainder != "" && strings.TrimSpace(remainder) != remainder {
				return "progress", strings.TrimSpace(remainder), true
			}
		}
	}
	return "", "", false
}

func parsePlayerTaskArguments(arguments string) (int, string) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return 1, ""
	}
	if page, err := strconv.Atoi(arguments); err == nil && page > 0 {
		return page, ""
	}
	return 1, arguments
}

func formatPlayerTaskProgress(page PlayerProgressPage, pageNumber int) string {
	if page.Total == 0 {
		if page.Query != "" {
			return fmt.Sprintf("【任务】没有找到与“%s”匹配的任务。", page.Query)
		}
		return "【任务】当前没有已启用的任务。"
	}
	pages := (page.Total + page.Limit - 1) / page.Limit
	if pages < 1 {
		pages = 1
	}
	lines := []string{fmt.Sprintf("【任务】第%d/%d页", pageNumber, pages)}
	for _, item := range page.Items {
		status := fmt.Sprintf("%d/%d", item.Progress, item.TargetAmount)
		if item.Completed {
			status = "已完成"
		}
		reward := "无积分奖励"
		if item.RewardPoints > 0 {
			reward = fmt.Sprintf("奖励%d积分", item.RewardPoints)
			if item.RewardGranted {
				reward += "（已发放）"
			} else if item.RewardStatus == "failed" {
				reward += "（发放失败，等待管理员重试）"
			}
		}
		lines = append(lines, fmt.Sprintf("%s｜%s｜%s｜%s", item.TaskName, status, playerCycleLabel(item.Cycle), reward))
	}
	lines = append(lines, "输入“任务 2”翻页，输入“任务 关键词”搜索；奖励达标后自动发放。")
	return strings.Join(lines, "\n")
}

func playerCycleLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "daily":
		return "每日"
	case "weekly":
		return "每周"
	case "once":
		return "永久一次"
	default:
		return value
	}
}
