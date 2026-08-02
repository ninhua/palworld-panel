package api

import (
	"context"

	"palpanel/internal/economy"
	"palpanel/internal/gameevents"
	"palpanel/internal/shop"
)

type gameChatCommandOutcome struct {
	Economy   *economy.CommandResult
	Shop      *shop.PlayerCommandResult
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
	outcome.Handled = shopCommand.Handled
	outcome.Duplicate = shopCommand.Duplicate
	outcome.Reply = shopCommand.Reply
	return outcome, nil
}
