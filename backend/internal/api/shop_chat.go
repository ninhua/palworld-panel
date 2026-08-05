package api

import (
	"context"

	"palpanel/internal/economy"
	"palpanel/internal/gameevents"
	"palpanel/internal/shop"
	"palpanel/internal/tasks"
)

type gameChatCommandOutcome struct {
	Economy   *economy.CommandResult
	Shop      *shop.PlayerCommandResult
	Boss      *bossRegistrationChatResult
	Tasks     *tasks.PlayerCommandResult
	Handled   bool
	Duplicate bool
	Reply     string
}

func (s Server) executeGameChatCommand(ctx context.Context, record gameevents.Record) (gameChatCommandOutcome, error) {
	message, _ := record.Payload["message"].(string)
	pointService, err := s.economyService()
	if err != nil {
		return gameChatCommandOutcome{}, err
	}
	pointCommand, err := pointService.ExecuteCommandDetailed(ctx, economy.CommandRequest{
		EventID: record.EventID, PlayerUID: record.PlayerUID, Nickname: record.Nickname,
		SteamID: record.SteamID, Message: message,
	})
	if err != nil {
		return gameChatCommandOutcome{}, err
	}
	outcome := gameChatCommandOutcome{Economy: &pointCommand}
	if pointCommand.Handled {
		outcome.Handled = true
		outcome.Duplicate = pointCommand.Duplicate
		outcome.Reply = pointCommand.Reply
		return outcome, nil
	}

	config, err := pointService.DetailedConfig(ctx)
	if err != nil {
		return gameChatCommandOutcome{}, err
	}
	shopService, err := s.shopService()
	if err != nil {
		return gameChatCommandOutcome{}, err
	}
	shopCommand, err := shopService.ExecutePlayerCommand(ctx, pointService, shop.NewPalDefenderDispatcher(s.defender), shop.PlayerCommandRequest{
		EventID: record.EventID, PlayerUID: record.PlayerUID, Nickname: record.Nickname, SteamID: record.SteamID,
		Message: message, CommandPrefix: config.CommandPrefix, AllowBareCommand: config.AllowBareCommands,
	}, "game-chat:"+record.PlayerUID)
	if err != nil {
		return gameChatCommandOutcome{}, err
	}
	outcome.Shop = &shopCommand
	if shopCommand.Handled {
		outcome.Handled = true
		outcome.Duplicate = shopCommand.Duplicate
		outcome.Reply = shopCommand.Reply
		return outcome, nil
	}

	bossCommand, err := s.executeBossRegistrationChatCommand(
		ctx,
		record,
		message,
		config.CommandPrefix,
		config.AllowBareCommands,
	)
	if err != nil {
		return gameChatCommandOutcome{}, err
	}
	outcome.Boss = &bossCommand
	if bossCommand.Handled {
		outcome.Handled = true
		outcome.Reply = bossCommand.Reply
		return outcome, nil
	}

	taskService, err := s.taskService()
	if err != nil {
		return gameChatCommandOutcome{}, err
	}
	taskCommand, err := taskService.ExecutePlayerCommand(ctx, tasks.PlayerCommandRequest{
		PlayerUID: record.PlayerUID, Message: message, CommandPrefix: config.CommandPrefix, AllowBareCommand: config.AllowBareCommands,
	})
	if err != nil {
		return gameChatCommandOutcome{}, err
	}
	outcome.Tasks = &taskCommand
	outcome.Handled = taskCommand.Handled
	outcome.Reply = taskCommand.Reply
	return outcome, nil
}
