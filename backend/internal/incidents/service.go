package incidents

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
)

const (
	workerInterval       = 15 * time.Second
	maxDeliveryAttempts  = 6
	maxWebhookResponse   = 4096
	resolvedRetention    = 90 * 24 * time.Hour
	webhookEventName     = "palpanel.incident"
	webhookSchemaVersion = 1
)

type Service struct {
	cfg    appconfig.Config
	store  *db.Store
	client *http.Client
	now    func() time.Time
}

type Status struct {
	Enabled        bool   `json:"enabled"`
	TargetHost     string `json:"target_host,omitempty"`
	Signed         bool   `json:"signed"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxAttempts    int    `json:"max_attempts"`
}

type webhookEnvelope struct {
	SchemaVersion int               `json:"schema_version"`
	Event         string            `json:"event"`
	DeliveryID    string            `json:"delivery_id"`
	SentAt        string            `json:"sent_at"`
	Incident      db.Incident       `json:"incident"`
	Change        db.IncidentEvent  `json:"change"`
	Meta          map[string]string `json:"meta"`
}

func New(cfg appconfig.Config, store *db.Store) *Service {
	timeout := time.Duration(cfg.IncidentWebhookTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Service{
		cfg:   cfg,
		store: store,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("webhook redirects are disabled")
			},
		},
		now: time.Now,
	}
}

func (s *Service) Enabled() bool {
	return strings.TrimSpace(s.cfg.IncidentWebhookURL) != ""
}

func (s *Service) Status() Status {
	status := Status{Enabled: s.Enabled(), Signed: s.Enabled() && s.cfg.IncidentWebhookSecret != "", TimeoutSeconds: s.cfg.IncidentWebhookTimeoutSeconds, MaxAttempts: maxDeliveryAttempts}
	if parsed, err := url.Parse(s.cfg.IncidentWebhookURL); err == nil && status.Enabled {
		status.TargetHost = parsed.Hostname()
	}
	return status
}

func (s *Service) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	enabled := s.Enabled()
	if err := s.store.SetIncidentWebhookEnabled(context.Background(), enabled); err != nil {
		log.Printf("incident webhook state persistence failed: %v", err)
	}
	go func() {
		defer close(done)
		defer func() { _ = s.store.SetIncidentWebhookEnabled(context.Background(), false) }()
		if err := s.store.DeleteResolvedIncidentsBefore(context.Background(), s.now().UTC().Add(-resolvedRetention).Format(time.RFC3339Nano)); err != nil {
			log.Printf("incident retention cleanup failed: %v", err)
		}
		if enabled {
			s.drain(ctx)
		}
		ticker := time.NewTicker(workerInterval)
		defer ticker.Stop()
		pruneTicker := time.NewTicker(24 * time.Hour)
		defer pruneTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if enabled {
					s.drain(ctx)
				}
			case <-pruneTicker.C:
				if err := s.store.DeleteResolvedIncidentsBefore(ctx, s.now().UTC().Add(-resolvedRetention).Format(time.RFC3339Nano)); err != nil {
					log.Printf("incident retention cleanup failed: %v", err)
				}
			}
		}
	}()
	return done
}

func retryDelay(attempt int) time.Duration {
	delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}

func (s *Service) drain(ctx context.Context) {
	for batch := 0; batch < 5; batch++ {
		items, err := s.store.ListDueIncidentDeliveries(ctx, s.now().UTC().Format(time.RFC3339Nano), 20)
		if err != nil {
			log.Printf("incident delivery query failed: %v", err)
			return
		}
		if len(items) == 0 {
			return
		}
		for _, item := range items {
			if ctx.Err() != nil {
				return
			}
			if err := s.deliver(ctx, item); err != nil {
				attempt := item.Delivery.Attempts + 1
				final := attempt >= maxDeliveryAttempts
				next := s.now().UTC().Add(retryDelay(attempt)).Format(time.RFC3339Nano)
				if markErr := s.store.MarkIncidentDeliveryRetry(context.Background(), item.Delivery.ID, next, err.Error(), final); markErr != nil {
					log.Printf("incident delivery retry persistence failed: %v", markErr)
				}
				continue
			}
			if err := s.store.MarkIncidentDeliveryDelivered(context.Background(), item.Delivery.ID); err != nil {
				log.Printf("incident delivery completion persistence failed: %v", err)
			}
		}
	}
}

func (s *Service) deliver(ctx context.Context, item db.IncidentDeliveryPayload) error {
	return s.send(ctx, webhookEnvelope{
		SchemaVersion: webhookSchemaVersion,
		Event:         webhookEventName,
		DeliveryID:    item.Delivery.ID,
		SentAt:        s.now().UTC().Format(time.RFC3339Nano),
		Incident:      item.Incident,
		Change:        item.Event,
		Meta:          map[string]string{"product": "PalPanel", "version": "custom-stable"},
	})
}

func (s *Service) Test(ctx context.Context) error {
	if !s.Enabled() {
		return errors.New("incident webhook is disabled")
	}
	stamp := s.now().UTC().Format(time.RFC3339Nano)
	return s.send(ctx, webhookEnvelope{
		SchemaVersion: webhookSchemaVersion,
		Event:         "palpanel.incident.test",
		DeliveryID:    "test_" + strconv.FormatInt(s.now().UnixMilli(), 10),
		SentAt:        stamp,
		Incident: db.Incident{ID: "incident_test", Kind: "webhook_test", Severity: "info", Source: "system",
			Title: "PalPanel Webhook 测试", Summary: "这是一条由管理员触发的事件中心测试消息。", Status: "open",
			Occurrences: 1, FirstSeenAt: stamp, LastSeenAt: stamp, UpdatedAt: stamp},
		Change: db.IncidentEvent{ID: "event_test", IncidentID: "incident_test", Type: "opened", Actor: "administrator", CreatedAt: stamp},
		Meta:   map[string]string{"product": "PalPanel", "test": "true"},
	})
}

func (s *Service) send(ctx context.Context, payload webhookEnvelope) error {
	if !s.Enabled() {
		return errors.New("incident webhook is disabled")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	timestamp := strconv.FormatInt(s.now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.IncidentWebhookURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("webhook request configuration is invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PalPanel-Incident-Webhook/1")
	req.Header.Set("X-PalPanel-Event", payload.Event)
	req.Header.Set("X-PalPanel-Delivery", payload.DeliveryID)
	req.Header.Set("X-PalPanel-Timestamp", timestamp)
	mac := hmac.New(sha256.New, []byte(s.cfg.IncidentWebhookSecret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	req.Header.Set("X-PalPanel-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response, err := s.client.Do(req)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "redirect") {
			return errors.New("webhook redirect rejected")
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return errors.New("webhook request timed out")
		}
		return errors.New("webhook request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxWebhookResponse))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", response.StatusCode)
	}
	return nil
}
