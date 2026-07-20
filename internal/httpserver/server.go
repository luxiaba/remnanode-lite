package httpserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/netutil"

	"github.com/Luxiaba/remnanode-lite/internal/auth"
	"github.com/Luxiaba/remnanode-lite/internal/bodylimit"
	"github.com/Luxiaba/remnanode-lite/internal/config"
	"github.com/Luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/Luxiaba/remnanode-lite/internal/plugin"
	"github.com/Luxiaba/remnanode-lite/internal/secret"
	"github.com/Luxiaba/remnanode-lite/internal/stats"
	"github.com/Luxiaba/remnanode-lite/internal/xray"
)

type Server struct {
	httpServer     *http.Server
	maxConnections int
	xrayGate       xrayLifecycleGate
	manager        xrayController
	statsService   *stats.Service
	handlerService *nodehandler.Service
	pluginService  pluginController
	bodyBudget     *bodylimit.Budget
}

const (
	defaultMaxConnections = 128
	defaultMaxHandlers    = 32
	lowMemoryConnections  = 16
	lowMemoryHandlers     = 4
	maxBulkHandlers       = 1
	maxXrayStartHandlers  = 2
	maxRequestDuration    = 5 * time.Minute
)

type xrayController interface {
	Start(ctx context.Context, request xray.StartRequest) xray.StartResponse
	Stop() xray.StopResponse
	Health() xray.HealthResponse
}

type pluginController interface {
	ResetPluginsContext(ctx context.Context) error
	SyncContext(ctx context.Context, request *plugin.SyncPlugin) plugin.AcceptedResponse
	CollectReports() plugin.CollectReportsResponse
	BlockIPsContext(ctx context.Context, items []plugin.BlockIP) plugin.AcceptedResponse
	UnblockIPsContext(ctx context.Context, ips []string) plugin.AcceptedResponse
	RecreateTablesContext(ctx context.Context) plugin.AcceptedResponse
	ReportsCount() int
}

type Dependencies struct {
	Validator *auth.JWTValidator
	Xray      xrayController
	Stats     *stats.Service
	Handler   *nodehandler.Service
	Plugins   pluginController
	Body      *bodylimit.Budget
}

func (d Dependencies) validate() error {
	if d.Validator == nil {
		return errors.New("httpserver: JWT validator is required")
	}
	if d.Xray == nil {
		return errors.New("httpserver: Xray controller is required")
	}
	if d.Stats == nil {
		return errors.New("httpserver: stats service is required")
	}
	if d.Handler == nil {
		return errors.New("httpserver: handler service is required")
	}
	if d.Plugins == nil {
		return errors.New("httpserver: plugin controller is required")
	}
	if d.Body == nil {
		return errors.New("httpserver: request body budget is required")
	}
	return nil
}

func New(cfg config.Config, payload secret.Payload, dependencies Dependencies) (*Server, error) {
	if err := dependencies.validate(); err != nil {
		return nil, err
	}
	tlsConfig, err := buildTLSConfig(payload)
	if err != nil {
		return nil, err
	}

	server := &Server{
		manager:        dependencies.Xray,
		statsService:   dependencies.Stats,
		handlerService: dependencies.Handler,
		pluginService:  dependencies.Plugins,
		bodyBudget:     dependencies.Body,
	}

	maxConnections, maxHandlers := serverCapacity(cfg.LowMemory)
	protected := requireJWT(dependencies.Validator, requireKnownNodeRoute(withRequestTimeout(maxRequestDuration, server.nodeRequestHandler(maxHandlers))))

	server.maxConnections = maxConnections
	server.httpServer = &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           rejectUnknownPaths(protected),
		ErrorLog:          newHTTPErrorLogger(),
		TLSConfig:         tlsConfig,
		TLSNextProto:      map[string]func(*http.Server, *tls.Conn, http.Handler){},
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	return server, nil
}

func (s *Server) nodeRequestHandler(maxHandlers int) http.Handler {
	nodeRoutes := withNodeRequestBodyLimit(
		s.bodyBudget,
		s.bodyBudget.DecompressMiddleware(s.bodyBudget.LimitMiddleware(http.HandlerFunc(s.handleNodeRoutes))),
	)
	limited := limitActiveHandlers(maxHandlers, nodeRoutes)
	startLimited := limitXrayStartRoutes(maxXrayStartHandlers, limited)
	return limitBulkNodeRoutes(maxBulkHandlers, startLimited)
}

func requireJWT(validator *auth.JWTValidator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := validator.ValidateBearer(r.Header.Get("Authorization")); err != nil {
			slog.Warn("dropping request with invalid JWT", "path", r.URL.Path, "remote", r.RemoteAddr)
			panic(http.ErrAbortHandler)
		}
		next.ServeHTTP(w, r)
	})
}

func rejectUnknownPaths(nodeHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/node/") {
			panic(http.ErrAbortHandler)
		}
		nodeHandler.ServeHTTP(w, r)
	})
}

func requireKnownNodeRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := lookupNodeRoute(r.Method, r.URL.Path); !ok {
			panic(http.ErrAbortHandler)
		}
		next.ServeHTTP(w, r)
	})
}

func withNodeRequestBodyLimit(budget *bodylimit.Budget, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, known := lookupNodeRoute(r.Method, r.URL.Path)
		if !known {
			route = 0
		}
		next.ServeHTTP(w, budget.WithRequestLimit(r, nodeRouteRequestBodyLimit(route)))
	})
}

func (s *Server) ListenAndServeTLS(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.httpServer.BaseContext = func(net.Listener) context.Context { return ctx }
	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	limited := netutil.LimitListener(listener, s.maxConnections)
	err = s.httpServer.ServeTLS(limited, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func serverCapacity(lowMemory bool) (connections, handlers int) {
	if lowMemory {
		return lowMemoryConnections, lowMemoryHandlers
	}
	return defaultMaxConnections, defaultMaxHandlers
}

func limitActiveHandlers(maxActive int, next http.Handler) http.Handler {
	if maxActive <= 0 {
		maxActive = 1
	}
	totalSlots := make(chan struct{}, maxActive)
	readCapacity := maxActive
	if readCapacity > 1 {
		readCapacity--
	}
	readSlots := make(chan struct{}, readCapacity)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, known := lookupNodeRoute(r.Method, r.URL.Path)
		readOnly := known && nodeRouteIsReadOnly(route)
		if readOnly {
			if !acquireRequestSlot(r.Context(), readSlots) {
				handleRequestWaitFailure(w, r)
				return
			}
			defer func() { <-readSlots }()
		}
		if !acquireRequestSlot(r.Context(), totalSlots) {
			handleRequestWaitFailure(w, r)
			return
		}
		defer func() { <-totalSlots }()
		next.ServeHTTP(w, r)
	})
}

func limitBulkNodeRoutes(maxActive int, next http.Handler) http.Handler {
	return limitSelectedNodeRoutes(maxActive, nodeRouteUsesBulkHandlerSlot, next)
}

func limitXrayStartRoutes(maxActive int, next http.Handler) http.Handler {
	return limitSelectedNodeRoutes(maxActive, func(route nodeRouteID) bool { return route == routeXrayStart }, next)
}

func limitSelectedNodeRoutes(maxActive int, usesSlot func(nodeRouteID) bool, next http.Handler) http.Handler {
	if maxActive <= 0 {
		maxActive = 1
	}
	slots := make(chan struct{}, maxActive)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, known := lookupNodeRoute(r.Method, r.URL.Path)
		if !known || !usesSlot(route) {
			next.ServeHTTP(w, r)
			return
		}
		if !acquireRequestSlot(r.Context(), slots) {
			handleRequestWaitFailure(w, r)
			return
		}
		defer func() { <-slots }()
		next.ServeHTTP(w, r)
	})
}

func acquireRequestSlot(ctx context.Context, slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		if ctx.Err() != nil {
			<-slots
			return false
		}
		return true
	case <-ctx.Done():
		return false
	}
}

func handleRequestWaitFailure(w http.ResponseWriter, r *http.Request) {
	if errors.Is(r.Context().Err(), context.Canceled) {
		panic(http.ErrAbortHandler)
	}
	writeRetryableServiceUnavailable(w, r)
}

func writeRetryableServiceUnavailable(w http.ResponseWriter, r *http.Request) {
	r.Close = true
	w.Header().Set("Connection", "close")
	w.Header().Set("Retry-After", "1")
	writeJSON(w, http.StatusServiceUnavailable, struct {
		StatusCode int    `json:"statusCode"`
		Message    string `json:"message"`
		Error      string `json:"error"`
	}{
		StatusCode: http.StatusServiceUnavailable,
		Message:    "request capacity unavailable",
		Error:      http.StatusText(http.StatusServiceUnavailable),
	})
}

type responseWriteTracker struct {
	http.ResponseWriter
	wrote bool
}

func (w *responseWriteTracker) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriteTracker) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriteTracker) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func withRequestTimeout(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parentContext := r.Context()
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		tracked := &responseWriteTracker{ResponseWriter: w}
		next.ServeHTTP(tracked, r.WithContext(ctx))
		if tracked.wrote {
			return
		}
		if errors.Is(parentContext.Err(), context.Canceled) {
			panic(http.ErrAbortHandler)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeRetryableServiceUnavailable(tracked, r)
		}
	})
}

func (s *Server) acquireXrayLifecycle(ctx context.Context) bool {
	return s.xrayGate.acquireExclusive(ctx)
}

func (s *Server) releaseXrayLifecycle() {
	s.xrayGate.releaseExclusive()
}

func (s *Server) acquireXrayStart(ctx context.Context) bool {
	return s.xrayGate.acquireStart(ctx)
}

func (s *Server) releaseXrayStart() {
	s.xrayGate.releaseStart()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) Close() error {
	return s.httpServer.Close()
}

func (s *Server) handleNodeRoutes(w http.ResponseWriter, r *http.Request) {
	route, ok := lookupNodeRoute(r.Method, r.URL.Path)
	if !ok {
		panic(http.ErrAbortHandler)
	}
	if !nodeRouteHasRequestDTO(route) && !s.validateNodeJSONDocument(w, r) {
		return
	}

	switch route {
	// xray
	case routeXrayHealthcheck:
		writeJSON(w, http.StatusOK, envelope[xray.HealthResponse]{Response: s.manager.Health()})
	case routeXrayStop:
		if !s.acquireXrayLifecycle(r.Context()) {
			handleRequestWaitFailure(w, r)
			return
		}
		defer s.releaseXrayLifecycle()
		response := s.manager.Stop()
		if response.IsStopped {
			if err := s.pluginService.ResetPluginsContext(r.Context()); err != nil {
				slog.Warn("failed to reset plugins after stopping Xray", "error", err)
			}
		}
		writeJSON(w, http.StatusOK, envelope[xray.StopResponse]{Response: response})
	case routeXrayStart:
		s.handleStart(w, r)

	// stats
	case routeStatsGetUserOnlineStatus:
		s.handleStatsGetUserOnlineStatus(w, r)
	case routeStatsGetSystemStats:
		s.handleStatsGetSystemStats(w, r)
	case routeStatsGetUsersStats:
		s.handleStatsGetUsersStats(w, r)
	case routeStatsGetInboundStats:
		s.handleStatsGetInboundStats(w, r)
	case routeStatsGetOutboundStats:
		s.handleStatsGetOutboundStats(w, r)
	case routeStatsGetAllInboundsStats:
		s.handleStatsGetAllInboundsStats(w, r)
	case routeStatsGetAllOutboundsStats:
		s.handleStatsGetAllOutboundsStats(w, r)
	case routeStatsGetCombinedStats:
		s.handleStatsGetCombinedStats(w, r)
	case routeStatsGetUserIPList:
		s.handleStatsGetUserIPList(w, r)
	case routeStatsGetUsersIPList:
		s.handleStatsGetUsersIPList(w, r)

	// handler
	case routeHandlerAddUser:
		s.handleAddUser(w, r)
	case routeHandlerRemoveUser:
		s.handleRemoveUser(w, r)
	case routeHandlerGetInboundUsersCount:
		s.handleGetInboundUsersCount(w, r)
	case routeHandlerGetInboundUsers:
		s.handleGetInboundUsers(w, r)
	case routeHandlerAddUsers:
		s.handleAddUsers(w, r)
	case routeHandlerRemoveUsers:
		s.handleRemoveUsers(w, r)
	case routeHandlerDropUsersConnections:
		s.handleDropUsersConnections(w, r)
	case routeHandlerDropIPs:
		s.handleDropIPs(w, r)

	// plugin
	case routePluginSync:
		s.handlePluginSync(w, r)
	case routePluginCollectTorrentReports:
		s.handlePluginCollectReports(w)
	case routePluginBlockIPs:
		s.handlePluginBlockIPs(w, r)
	case routePluginUnblockIPs:
		s.handlePluginUnblockIPs(w, r)
	case routePluginRecreateTables:
		s.handlePluginRecreateTables(w, r)
	}
}

func buildTLSConfig(payload secret.Payload) (*tls.Config, error) {
	certificate, err := tls.X509KeyPair([]byte(payload.NodeCertPEM), []byte(payload.NodeKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("load node TLS certificate: %w", err)
	}

	clientCAs := x509.NewCertPool()
	if ok := clientCAs.AppendCertsFromPEM([]byte(payload.CACertPEM)); !ok {
		return nil, errors.New("append client CA certificate: no certificates found")
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    clientCAs,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

type envelope[T any] struct {
	Response T `json:"response"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		slog.Warn("failed to write JSON response", "error", err)
	}
}
