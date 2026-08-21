package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luxiaba/remnanode-lite/internal/nodeapi"
)

type recordingGeoCheck struct {
	calls         int
	ip            string
	interfaceName string
	report        json.RawMessage
	err           error
}

func (service *recordingGeoCheck) Get(_ context.Context, ip, interfaceName string) (json.RawMessage, error) {
	service.calls++
	service.ip = ip
	service.interfaceName = interfaceName
	return service.report, service.err
}

func TestGeoCheckRouteForwardsOptionalBindingAndReport(t *testing.T) {
	t.Parallel()
	service := &recordingGeoCheck{report: json.RawMessage(`{"image":{"format":"svg","media_type":"image/svg+xml","encoding":"base64","data":"PHN2Zz4="},"country":"DE"}`)}
	server := &Server{geocheckService: service, bodyBudget: newHTTPTestBudget(t, false, 0)}
	request := newJSONRequest(http.MethodPost, "/node/stats/get-geocheck", strings.NewReader(`{"ip":"203.0.113.10","interface":"eth0"}`))
	response := httptest.NewRecorder()

	server.handleNodeRoutes(response, request)

	if response.Code != http.StatusOK || service.calls != 1 || service.ip != "203.0.113.10" || service.interfaceName != "eth0" {
		t.Fatalf("status/call = %d/%d, binding = %q/%q, body=%s", response.Code, service.calls, service.ip, service.interfaceName, response.Body.String())
	}
	var body struct {
		Response map[string]any `json:"response"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Response["country"] != "DE" {
		t.Fatalf("response = %s, decode error = %v", response.Body.Bytes(), err)
	}
}

func TestGeoCheckRouteValidatesOptionalStrings(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"ip":null}`, `{"interface":null}`, `{"ip":123}`, ``} {
		service := &recordingGeoCheck{}
		server := &Server{geocheckService: service, bodyBudget: newHTTPTestBudget(t, false, 0)}
		request := newJSONRequest(http.MethodPost, "/node/stats/get-geocheck", strings.NewReader(body))
		response := httptest.NewRecorder()
		server.handleNodeRoutes(response, request)
		if response.Code != http.StatusBadRequest || service.calls != 0 {
			t.Fatalf("body %q produced status/calls %d/%d: %s", body, response.Code, service.calls, response.Body.String())
		}
	}
}

func TestGeoCheckRouteWritesA018(t *testing.T) {
	t.Parallel()
	service := &recordingGeoCheck{err: nodeapi.ServiceError{Status: 500, Code: "A018", Message: "Geocheck failed"}}
	server := &Server{geocheckService: service, bodyBudget: newHTTPTestBudget(t, false, 0)}
	request := newJSONRequest(http.MethodPost, "/node/stats/get-geocheck", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	server.handleNodeRoutes(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"errorCode":"A018"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
