package paldefender

import (
	"context"
	"math"
	"strconv"
	"strings"
)

const maxTeleportCoordinate = 10_000_000

type RemoveItemsRequest struct {
	Items []ItemGrant `json:"Items"`
}

type TeleportRequest struct {
	Mode         string   `json:"Mode"`
	X            *float64 `json:"X,omitempty"`
	Y            *float64 `json:"Y,omitempty"`
	Z            *float64 `json:"Z,omitempty"`
	TargetPlayer string   `json:"TargetPlayer,omitempty"`
}

type ReleasePalRequest struct {
	PalID  string `json:"PalID"`
	Level  *int   `json:"Level,omitempty"`
	Gender string `json:"Gender,omitempty"`
	Rank   *int   `json:"Rank,omitempty"`
	Lucky  *bool  `json:"Lucky,omitempty"`
}

func (m Manager) RCONGetNearestBase(ctx context.Context, x, y, z float64) (RCONResult, error) {
	return m.runBaseRCON(ctx, "/getnearestbase", x, y, z)
}

func (m Manager) RCONKillNearestBase(ctx context.Context, x, y, z float64) (RCONResult, error) {
	return m.runBaseRCON(ctx, "/killnearestbase", x, y, z)
}

func (m Manager) runBaseRCON(ctx context.Context, command string, x, y, z float64) (RCONResult, error) {
	coordinates := []float64{x, y, z}
	for _, coordinate := range coordinates {
		if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || math.Abs(coordinate) > maxTeleportCoordinate {
			return RCONResult{}, invalidRESTRequest("base coordinates must be finite and between -%d and %d", maxTeleportCoordinate, maxTeleportCoordinate)
		}
	}
	return m.runTypedRCON(ctx, strings.Join([]string{command, formatRCONFloat(x), formatRCONFloat(y), formatRCONFloat(z)}, " "), false)
}

func (m Manager) RCONRemoveItems(ctx context.Context, identifier string, request RemoveItemsRequest) (RCONResult, error) {
	identifier, err := validatePlayerIdentifier(identifier)
	if err != nil {
		return RCONResult{}, err
	}
	if len(request.Items) == 0 || len(request.Items) > maxItemGrants {
		return RCONResult{}, invalidRESTRequest("Items must contain between 1 and %d entries", maxItemGrants)
	}
	parts := make([]string, 0, len(request.Items))
	seen := map[string]bool{}
	for index := range request.Items {
		itemID := strings.TrimSpace(request.Items[index].ItemID)
		if !itemIdentifierPattern.MatchString(itemID) {
			return RCONResult{}, invalidRESTRequest("item %d has an invalid ItemID", index+1)
		}
		if request.Items[index].Count <= 0 || request.Items[index].Count > 2_147_483_647 {
			return RCONResult{}, invalidRESTRequest("item %d Count must be between 1 and 2147483647", index+1)
		}
		key := strings.ToLower(itemID)
		if seen[key] {
			return RCONResult{}, invalidRESTRequest("item %d duplicates ItemID %q", index+1, itemID)
		}
		seen[key] = true
		parts = append(parts, itemID+":"+strconv.FormatInt(request.Items[index].Count, 10))
	}
	command := "/delitems " + identifier + " " + strings.Join(parts, " ")
	if len(command) > 4096 {
		return RCONResult{}, invalidRESTRequest("item removal command exceeds the RCON command limit")
	}
	return m.runTypedRCON(ctx, command, false)
}

func (m Manager) RCONTeleport(ctx context.Context, identifier string, request TeleportRequest) (RCONResult, error) {
	identifier, err := validatePlayerIdentifier(identifier)
	if err != nil {
		return RCONResult{}, err
	}
	switch strings.ToLower(strings.TrimSpace(request.Mode)) {
	case "coordinates":
		if request.X == nil || request.Y == nil || strings.TrimSpace(request.TargetPlayer) != "" {
			return RCONResult{}, invalidRESTRequest("coordinate teleport requires X and Y and does not accept TargetPlayer")
		}
		coordinates := []*float64{request.X, request.Y, request.Z}
		for _, coordinate := range coordinates {
			if coordinate != nil && (math.IsNaN(*coordinate) || math.IsInf(*coordinate, 0) || math.Abs(*coordinate) > maxTeleportCoordinate) {
				return RCONResult{}, invalidRESTRequest("teleport coordinates must be finite and between -%d and %d", maxTeleportCoordinate, maxTeleportCoordinate)
			}
		}
		parts := []string{"/tp", identifier, formatRCONFloat(*request.X), formatRCONFloat(*request.Y)}
		if request.Z != nil {
			parts = append(parts, formatRCONFloat(*request.Z))
		}
		return m.runTypedRCON(ctx, strings.Join(parts, " "), false)
	case "player":
		if request.X != nil || request.Y != nil || request.Z != nil {
			return RCONResult{}, invalidRESTRequest("player teleport does not accept coordinates")
		}
		target, err := validatePlayerIdentifier(request.TargetPlayer)
		if err != nil {
			return RCONResult{}, invalidRESTRequest("invalid TargetPlayer")
		}
		if strings.EqualFold(identifier, target) {
			return RCONResult{}, invalidRESTRequest("TargetPlayer must be different from the source player")
		}
		return m.runTypedRCON(ctx, "/tp "+identifier+" "+target, false)
	default:
		return RCONResult{}, invalidRESTRequest("Mode must be coordinates or player")
	}
}

func (m Manager) RCONReleasePal(ctx context.Context, identifier string, request ReleasePalRequest) (RCONResult, error) {
	identifier, err := validatePlayerIdentifier(identifier)
	if err != nil {
		return RCONResult{}, err
	}
	request.PalID = strings.TrimSpace(request.PalID)
	if !itemIdentifierPattern.MatchString(request.PalID) {
		return RCONResult{}, invalidRESTRequest("invalid PalID")
	}
	filters := []string{"ID", request.PalID}
	if request.Level != nil {
		if *request.Level < 1 || *request.Level > 255 {
			return RCONResult{}, invalidRESTRequest("Level must be between 1 and 255")
		}
		filters = append(filters, "Level="+strconv.Itoa(*request.Level))
	}
	if request.Gender != "" {
		gender := strings.ToLower(strings.TrimSpace(request.Gender))
		if gender != "male" && gender != "female" {
			return RCONResult{}, invalidRESTRequest("Gender must be male or female")
		}
		filters = append(filters, "Gender", gender)
	}
	if request.Rank != nil {
		if *request.Rank < 0 || *request.Rank > 255 {
			return RCONResult{}, invalidRESTRequest("Rank must be between 0 and 255")
		}
		filters = append(filters, "Rank="+strconv.Itoa(*request.Rank))
	}
	if request.Lucky != nil {
		filters = append(filters, "Lucky", strconv.FormatBool(*request.Lucky))
	}
	filters = append(filters, "Limit", "1")
	return m.runTypedRCON(ctx, "/deletepals "+identifier+" "+strings.Join(filters, " "), false)
}

func formatRCONFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
