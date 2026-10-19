package httpserver

import (
	"net/http"

	"github.com/luxiaba/remnanode-lite/internal/nodeapi"
	"github.com/luxiaba/remnanode-lite/internal/plugin"
	"github.com/luxiaba/remnanode-lite/internal/xray"
)

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var request nodeapi.XrayStartRequest
	if !s.decodeNodeRequest(w, r, &request) {
		return
	}

	inbounds := make([]xray.InboundHash, 0, len(*request.Internals.Hashes.Inbounds))
	for _, inbound := range *request.Internals.Hashes.Inbounds {
		inbounds = append(inbounds, xray.InboundHash{
			UsersCount: *inbound.UsersCount,
			Hash:       *inbound.Hash,
			Tag:        *inbound.Tag,
		})
	}
	command := xray.StartRequest{
		Internals: xray.StartInternals{
			ForceRestart: request.Internals.ForceRestart.Value,
			Hashes: xray.ConfigHash{
				EmptyConfig: *request.Internals.Hashes.EmptyConfig,
				Inbounds:    inbounds,
			},
		},
		XrayConfig: *request.XrayConfig,
	}
	if !s.acquireXrayStart(r.Context()) {
		handleRequestWaitFailure(w, r)
		return
	}
	defer s.releaseXrayStart()
	response := s.manager.Start(r.Context(), command)
	if response.IsStarted && response.CoreStarted {
		var metadata *plugin.NodeMetadata
		if request.Internals.Metadata.Present {
			value := request.Internals.Metadata.Value
			metadata = &plugin.NodeMetadata{
				Name: *value.Name, UUID: *value.UUID, ID: *value.ID,
				Tags: *value.Tags, CountryCode: *value.CountryCode,
			}
		}
		// The shared lifecycle lease still protects the committed plugin
		// configuration. Notification admission is bounded and never delays
		// the start response for an external HTTP delivery.
		s.pluginService.NotifyCoreStarted(metadata)
	}
	writeNodeResponse(w, response)
}
