package shop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"palpanel/internal/paldefender"
)

type palDefenderDispatcher struct {
	manager paldefender.Manager
}

func NewPalDefenderDispatcher(manager paldefender.Manager) Dispatcher {
	return palDefenderDispatcher{manager: manager}
}

func (d palDefenderDispatcher) ResolvePlayer(ctx context.Context, aliases []string) (string, error) {
	wanted := map[string]bool{}
	for _, alias := range aliases {
		if normalized := deliveryIdentity(alias); normalized != "" {
			wanted[normalized] = true
		}
	}
	if len(wanted) == 0 {
		return "", ErrDeliveryPlayerMissing
	}
	response, err := d.manager.RESTPlayers(ctx)
	if err != nil {
		return "", err
	}
	for _, player := range response.Players {
		if !wanted[deliveryIdentity(player.UserID)] && !wanted[deliveryIdentity(player.PlayerUID)] {
			continue
		}
		if identifier := strings.TrimSpace(player.UserID); identifier != "" {
			return identifier, nil
		}
		if identifier := strings.TrimSpace(player.PlayerUID); identifier != "" {
			return identifier, nil
		}
	}
	return "", ErrDeliveryPlayerMissing
}

func (d palDefenderDispatcher) GiveItems(ctx context.Context, player string, items []ItemGrant) error {
	request := paldefender.GiveItemsRequest{Items: make([]paldefender.ItemGrant, 0, len(items))}
	for _, item := range items {
		request.Items = append(request.Items, paldefender.ItemGrant{ItemID: item.ItemID, Count: item.Count})
	}
	_, err := d.manager.RESTGiveItems(ctx, player, request)
	return classifyPalDefenderDeliveryError(err)
}

func (d palDefenderDispatcher) GivePalTemplates(ctx context.Context, player string, templates []string) error {
	_, err := d.manager.RESTGivePalTemplates(ctx, player, paldefender.GivePalTemplatesRequest{PalTemplates: templates})
	return classifyPalDefenderDeliveryError(err)
}

func classifyPalDefenderDeliveryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, paldefender.ErrRESTTimeout) ||
		errors.Is(err, paldefender.ErrRESTUnavailable) ||
		errors.Is(err, paldefender.ErrRESTInvalidResponse) ||
		errors.Is(err, paldefender.ErrRESTResponseTooLarge) {
		return fmt.Errorf("%w: %v", ErrDeliveryUncertain, err)
	}
	var restErr *paldefender.RESTError
	if errors.As(err, &restErr) && restErr.Status >= http.StatusInternalServerError {
		return fmt.Errorf("%w: %v", ErrDeliveryUncertain, err)
	}
	return err
}

func deliveryIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	return value
}
