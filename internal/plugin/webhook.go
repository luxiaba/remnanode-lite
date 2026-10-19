package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const outboundWebhookTimeout = 5 * time.Second

type webhookDelivery struct {
	url           string
	body          []byte
	reportFailure bool
}

func newWebhookDelivery(url string, payload any, reportFailure bool) *webhookDelivery {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return &webhookDelivery{url: url, body: body, reportFailure: reportFailure}
}

// Only the queue worker starts deliveries. Its final Wait therefore cannot
// race with Add, and both notification types share the same concurrency bound.
func (s *Service) dispatchWebhook(delivery webhookDelivery) {
	if s.webhookContext.Err() != nil {
		return
	}
	select {
	case s.outboundWebhookSlots <- struct{}{}:
	default:
		if delivery.reportFailure {
			slog.Warn("post-start webhook skipped: delivery capacity unavailable")
		}
		return
	}
	s.outboundWebhookWG.Add(1)
	go func() {
		defer s.outboundWebhookWG.Done()
		defer func() { <-s.outboundWebhookSlots }()
		s.sendWebhook(s.webhookContext, delivery)
	}()
}

func (s *Service) sendWebhook(ctx context.Context, delivery webhookDelivery) {
	deliveryCtx, cancel := context.WithTimeout(ctx, outboundWebhookTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(deliveryCtx, http.MethodPost, delivery.url, bytes.NewReader(delivery.body))
	if err == nil {
		request.Header.Set("Content-Type", "application/json")
		var response *http.Response
		response, err = http.DefaultClient.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if delivery.reportFailure && (response.StatusCode < 200 || response.StatusCode >= 300) {
				slog.Warn("post-start webhook failed", "status", response.StatusCode)
			}
		}
	}
	if err != nil && delivery.reportFailure {
		// URL errors may contain credentials or query tokens. Keep the log
		// diagnostic bounded and omit the configured URL and metadata.
		slog.Warn("post-start webhook failed: request could not be completed")
	}
}
