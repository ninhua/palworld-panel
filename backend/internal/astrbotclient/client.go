package astrbotclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/appconfig"
)

type Client struct {
	cfg    appconfig.Config
	client *http.Client
}

type TicketIdentity struct {
	QQID      string `json:"qq_id"`
	PlayerUID string `json:"player_uid"`
	Nickname  string `json:"nickname"`
	Balance   int    `json:"balance"`
	Status    string `json:"status"`
}

func New(cfg appconfig.Config) *Client {
	return &Client{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) Enabled() bool { return strings.TrimSpace(c.cfg.AstrBotSharedSecret) != "" }

func (c *Client) SyncCatalog(ctx context.Context, fingerprint string, players []map[string]any) error {
	_, err := c.post(ctx, "/v1/catalog/sync", map[string]any{"fingerprint": fingerprint, "players": players})
	return err
}

func (c *Client) ExchangeTicket(ctx context.Context, ticket string) (TicketIdentity, error) {
	raw, err := c.post(ctx, "/v1/tickets/exchange", map[string]any{"ticket": ticket})
	if err != nil {
		return TicketIdentity{}, err
	}
	var identity TicketIdentity
	err = json.Unmarshal(raw, &identity)
	return identity, err
}

func (c *Client) Reserve(ctx context.Context, qqID, referenceID string, amount int) (string, int, error) {
	payload := map[string]any{"qq_id": qqID, "reference_id": referenceID}
	if amount > 0 {
		payload["amount"] = amount
	}
	raw, err := c.post(ctx, "/v1/credits/reserve", payload)
	if err != nil {
		return "", 0, err
	}
	var response struct {
		OK            bool   `json:"ok"`
		ReservationID string `json:"reservation_id"`
		Balance       int    `json:"balance"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", 0, err
	}
	if !response.OK {
		return "", response.Balance, fmt.Errorf("insufficient points")
	}
	return response.ReservationID, response.Balance, nil
}

func (c *Client) Settle(ctx context.Context, reservationID string, commit bool) error {
	path := "/v1/credits/release"
	if commit {
		path = "/v1/credits/commit"
	}
	_, err := c.post(ctx, path, map[string]any{"reservation_id": reservationID})
	return err
}

func (c *Client) post(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("AstrBot integration is not configured")
	}
	endpoint, err := url.Parse(c.cfg.AstrBotPluginURL)
	if err != nil || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("AstrBot plugin URL is invalid")
	}
	switch strings.ToLower(endpoint.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("AstrBot plugin URL must use HTTP or HTTPS")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.AstrBotPluginURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonceBytes := make([]byte, 18)
	_, _ = rand.Read(nonceBytes)
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PalPanel-Id", c.cfg.AstrBotPanelID)
	req.Header.Set("X-PalPanel-Timestamp", timestamp)
	req.Header.Set("X-PalPanel-Nonce", nonce)
	req.Header.Set("X-PalPanel-Signature", sign(c.cfg.AstrBotSharedSecret, req.Method, path, timestamp, nonce, body))
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AstrBot plugin returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func Verify(secret, method, path, timestamp, nonce, supplied string, body []byte) bool {
	parsed, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(parsed, 0)) > time.Minute || time.Until(time.Unix(parsed, 0)) > time.Minute {
		return false
	}
	expected := sign(secret, method, path, timestamp, nonce, body)
	return nonce != "" && hmac.Equal([]byte(expected), []byte(supplied))
}

func sign(secret, method, path, timestamp, nonce string, body []byte) string {
	digest := sha256.Sum256(body)
	canonical := strings.Join([]string{strings.ToUpper(method), path, timestamp, nonce, hex.EncodeToString(digest[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}
