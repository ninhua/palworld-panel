package economy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"palpanel/internal/playeridentity"
)

var ErrShopAccountIdentityAmbiguous = errors.New("shop account identity is ambiguous")

type ShopAccountCandidate struct {
	PlayerUID string `json:"player_uid"`
	SteamID   string `json:"steam_id,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
	Balance   int64  `json:"balance"`
}

type ShopAccountResolution struct {
	RequestedPlayerUID string                 `json:"requested_player_uid"`
	RequestedSteamID   string                 `json:"requested_steam_id,omitempty"`
	CanonicalPlayerUID string                 `json:"canonical_player_uid"`
	CanonicalSteamID   string                 `json:"canonical_steam_id,omitempty"`
	MatchStrategy      string                 `json:"match_strategy"`
	Warning            string                 `json:"warning,omitempty"`
	Account            Account                `json:"account"`
	ReservedPoints     int64                  `json:"reserved_points"`
	Candidates         []ShopAccountCandidate `json:"candidates,omitempty"`
}

// ResolveShopAccount resolves the points account used by a shop operation.
// A unique SteamID match may replace a zero-balance PlayerUID account, which
// handles game events that expose a different PlayerUID representation from
// the one previously associated with the player's points account.
func (s *Service) ResolveShopAccount(ctx context.Context, playerUID, nickname, steamID string) (ShopAccountResolution, error) {
	requestedUID := strings.TrimSpace(playerUID)
	requestedSteam := strings.TrimSpace(steamID)
	canonicalUID := playeridentity.Normalize(requestedUID)
	canonicalSteam := playeridentity.NormalizeSteamID(requestedSteam)
	if canonicalUID == "" && canonicalSteam == "" {
		return ShopAccountResolution{}, ErrInvalidPlayerUID
	}

	resolution := ShopAccountResolution{
		RequestedPlayerUID: requestedUID,
		RequestedSteamID:   requestedSteam,
		CanonicalPlayerUID: canonicalUID,
		CanonicalSteamID:   canonicalSteam,
	}

	exact, exactFound, err := s.shopAccountByCanonicalUID(ctx, canonicalUID)
	if err != nil {
		return ShopAccountResolution{}, err
	}
	candidates, err := s.shopAccountsBySteam(ctx, canonicalSteam)
	if err != nil {
		return ShopAccountResolution{}, err
	}
	resolution.Candidates = make([]ShopAccountCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		resolution.Candidates = append(resolution.Candidates, ShopAccountCandidate{
			PlayerUID: candidate.PlayerUID, SteamID: candidate.SteamID, Nickname: candidate.Nickname, Balance: candidate.Balance,
		})
	}

	otherCandidates := make([]Account, 0, len(candidates))
	for _, candidate := range candidates {
		if exactFound && playeridentity.Equivalent(candidate.PlayerUID, exact.PlayerUID) {
			continue
		}
		otherCandidates = append(otherCandidates, candidate)
	}

	choose := func(account Account, strategy, warning string) (ShopAccountResolution, error) {
		resolution.Account = account
		resolution.MatchStrategy = strategy
		resolution.Warning = warning
		reserved, reserveErr := s.ShopReservedPoints(ctx, account.PlayerUID)
		if reserveErr != nil {
			return ShopAccountResolution{}, reserveErr
		}
		resolution.ReservedPoints = reserved
		return resolution, nil
	}

	if exactFound {
		if len(otherCandidates) == 1 && exact.Balance == 0 && otherCandidates[0].Balance > 0 {
			return choose(otherCandidates[0], "steam_id_fallback", "当前事件 PlayerUID 命中零余额账户，已使用 SteamID 唯一匹配的已有积分账户。")
		}
		warning := ""
		if len(otherCandidates) > 0 {
			warning = "PlayerUID 与 SteamID 指向不同积分账户；本次优先使用 PlayerUID 账户，请在面板核对身份。"
		}
		return choose(exact, "player_uid", warning)
	}

	if len(otherCandidates) == 1 {
		return choose(otherCandidates[0], "steam_id", "未找到 PlayerUID 账户，已使用 SteamID 唯一匹配的已有积分账户。")
	}
	if len(otherCandidates) > 1 {
		resolution.MatchStrategy = "ambiguous"
		return resolution, fmt.Errorf("%w: SteamID %s matched %d accounts", ErrShopAccountIdentityAmbiguous, canonicalSteam, len(otherCandidates))
	}

	accountKey := canonicalUID
	if accountKey == "" {
		accountKey = canonicalSteam
	}
	account, err := s.EnsureAccount(ctx, accountKey, nickname, canonicalSteam)
	if err != nil {
		return ShopAccountResolution{}, err
	}
	return choose(account, "created", "未找到已有积分账户，已创建新账户。")
}

func (s *Service) ShopReservedPoints(ctx context.Context, playerUID string) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM economy_reservations WHERE pal_uid_canonical(player_uid)=pal_uid_canonical(?) AND status='reserved'`, playerUID).Scan(&total)
	return total, err
}

func (s *Service) shopAccountByCanonicalUID(ctx context.Context, playerUID string) (Account, bool, error) {
	if strings.TrimSpace(playerUID) == "" {
		return Account{}, false, nil
	}
	account, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT player_uid,nickname,steam_id,status,balance,created_at,updated_at FROM economy_accounts WHERE pal_uid_canonical(player_uid)=pal_uid_canonical(?) ORDER BY updated_at DESC LIMIT 1`, playerUID))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, false, nil
	}
	return account, err == nil, err
}

func (s *Service) shopAccountsBySteam(ctx context.Context, steamID string) ([]Account, error) {
	steamID = playeridentity.NormalizeSteamID(steamID)
	if steamID == "" {
		return nil, nil
	}
	raw := strings.TrimPrefix(steamID, "steam_")
	rows, err := s.db.QueryContext(ctx, `SELECT player_uid,nickname,steam_id,status,balance,created_at,updated_at FROM economy_accounts
		WHERE lower(trim(steam_id)) IN (?,?) OR lower(trim(player_uid)) IN (?,?)
		ORDER BY balance DESC,updated_at DESC`, steamID, raw, steamID, raw)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Account, 0)
	seen := map[string]bool{}
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		key := playeridentity.Normalize(account.PlayerUID)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, account)
	}
	return items, rows.Err()
}
