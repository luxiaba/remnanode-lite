package plugin

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// Bound queued metadata before JSON encoding; escaping adds at most six bytes
// per input byte. This keeps a full notification queue within a small budget.
const maxPostStartMetadataBytes = 8 << 10

type postStartSettings struct {
	enabled        bool
	webhookEnabled bool
	url            string
}

// NodeMetadata is the public metadata carried by service.core_started events.
// Unknown Panel DTO fields never enter this event.
type NodeMetadata struct {
	Name        string   `json:"name"`
	UUID        string   `json:"uuid"`
	ID          float64  `json:"id"`
	Tags        []string `json:"tags"`
	CountryCode string   `json:"countryCode"`
}

// NotifyCoreStarted is called under the HTTP lifecycle lease only after a new
// core has become ready. Snapshot URL and payload before returning so a later
// plugin sync or caller mutation cannot redirect an already admitted event.
func (s *Service) NotifyCoreStarted(metadata *NodeMetadata) {
	snapshot := s.state.currentSnapshot()
	if snapshot == nil || !snapshot.postStart.enabled || !snapshot.postStart.webhookEnabled {
		return
	}
	var eventMetadata any = struct{}{}
	if metadata != nil {
		size := len(metadata.Name) + len(metadata.UUID) + len(metadata.CountryCode)
		for _, tag := range metadata.Tags {
			size += len(tag) + 3
		}
		if size > maxPostStartMetadataBytes {
			slog.Warn("post-start webhook skipped: metadata budget exceeded")
			return
		}
		copy := *metadata
		if copy.Tags == nil {
			copy.Tags = []string{}
		}
		eventMetadata = copy
	}
	delivery := newWebhookDelivery(snapshot.postStart.url, struct {
		Scope     string `json:"scope"`
		Event     string `json:"event"`
		Timestamp string `json:"timestamp"`
		Metadata  any    `json:"metadata"`
	}{"service", "service.core_started", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), eventMetadata}, true)
	if delivery == nil {
		return
	}
	s.webhookAdmissionMu.RLock()
	defer s.webhookAdmissionMu.RUnlock()
	if s.webhookStopped.Load() {
		return
	}
	select {
	case s.webhookQueue <- queuedWebhook{delivery: delivery}:
	default:
		slog.Warn("post-start webhook skipped: queue capacity unavailable")
	}
}

func validatePostStartSection(raw any) error {
	section, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("postStart must be an object")
	}
	if enabled, present := section["enabled"]; present {
		if _, ok := enabled.(bool); !ok {
			return fmt.Errorf("postStart.enabled must be a boolean")
		}
	}
	webhookRaw, present := section["webhook"]
	if !present {
		return nil
	}
	webhook, ok := webhookRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("postStart.webhook must be an object")
	}
	if _, ok := webhook["enabled"].(bool); !ok {
		return fmt.Errorf("postStart.webhook.enabled is required and must be a boolean")
	}
	value, ok := webhook["url"].(string)
	if !ok {
		return fmt.Errorf("postStart.webhook.url is required and must be an HTTP(S) URL")
	}
	if err := validateStringLength("postStart.webhook.url", value); err != nil {
		return err
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("postStart.webhook.url must be an HTTP(S) URL")
	}
	return nil
}
