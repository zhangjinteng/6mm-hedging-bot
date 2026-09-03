package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/exchange"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/observability"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/service"
	"github.com/zhangjinteng/6mm-hedging-bot/internal/tasks"
)

type Server struct {
	router          *gin.Engine
	hedge           *service.HedgeService
	mgmt            *mgmt.Repository
	queue           *tasks.Enqueuer
	adapter         exchange.Adapter
	symbolAllowlist service.SymbolAllowlistProvider
	exposureSync    *service.ExposureSyncService
	positionSync    *service.PositionSyncService
	hedgeMonitor    *service.HedgeMonitorService
	reconciliation  *service.ExchangeReconciliationService
}

func NewServer(hedgeService *service.HedgeService, mgmtRepo *mgmt.Repository, queue *tasks.Enqueuer, adapters ...exchange.Adapter) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(metricsMiddleware(), gin.Recovery())

	var adapter exchange.Adapter
	if len(adapters) > 0 {
		adapter = adapters[0]
	}

	server := &Server{
		router:  router,
		hedge:   hedgeService,
		mgmt:    mgmtRepo,
		queue:   queue,
		adapter: adapter,
	}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) SetSymbolAllowlistProvider(provider service.SymbolAllowlistProvider) {
	s.symbolAllowlist = provider
}

func (s *Server) SetExposureSyncService(syncService *service.ExposureSyncService) {
	s.exposureSync = syncService
}

func (s *Server) SetPositionSyncService(syncService *service.PositionSyncService) {
	s.positionSync = syncService
}

func (s *Server) SetHedgeMonitorService(monitorService *service.HedgeMonitorService) {
	s.hedgeMonitor = monitorService
}

func (s *Server) SetExchangeReconciliationService(reconciliationService *service.ExchangeReconciliationService) {
	s.reconciliation = reconciliationService
}

func (s *Server) routes() {
	s.router.GET("/", serveIndexHTML)
	s.router.GET("/index.html", serveIndexHTML)

	s.router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	s.router.GET("/metrics", gin.WrapH(observability.MetricsHandler()))

	v1 := s.router.Group("/api/v1")
	v1.POST("/exchange-accounts", s.createExchangeAccount)
	v1.GET("/exchange-accounts", s.listExchangeAccounts)
	v1.GET("/exchange-accounts/options", s.listExchangeAccountOptions)
	v1.GET("/exchange-accounts/:id", s.getExchangeAccount)
	v1.PATCH("/exchange-accounts/:id", s.updateExchangeAccount)
	v1.DELETE("/exchange-accounts/:id", s.deleteExchangeAccount)
	v1.POST("/exchange-accounts/:id/primary", s.setPrimaryExchangeAccount)
	v1.GET("/exchange-markets", s.listExchangeMarkets)
	v1.POST("/exchange-markets/sync", s.syncExchangeMarkets)
	adapter := v1.Group("/exchange-adapter")
	adapter.POST("/fetch-balance", s.fetchAdapterBalance)
	adapter.POST("/fetch-positions", s.fetchAdapterPositions)
	adapter.POST("/close-positions", s.closeAdapterPositions)
	adapter.POST("/fetch-ticker", s.fetchAdapterTicker)
	adapter.POST("/fetch-markets", s.fetchAdapterMarkets)
	adapter.POST("/place-order", s.placeAdapterOrder)
	adapter.POST("/fetch-order", s.fetchAdapterOrder)
	adapter.POST("/cancel-order", s.cancelAdapterOrder)
	adapter.POST("/set-leverage", s.setAdapterLeverage)
	adapter.POST("/set-margin-mode", s.setAdapterMarginMode)
	v1.POST("/hedge-configs", s.createHedgeConfig)
	v1.GET("/hedge-configs", s.listHedgeConfigs)
	v1.POST("/exposures", s.upsertExposure)
	v1.POST("/exposures/sync", s.syncExposures)
	v1.POST("/positions", s.upsertPosition)
	v1.POST("/positions/sync", s.syncPositions)
	v1.GET("/hedge-monitor", s.listHedgeMonitor)
	v1.POST("/hedge-monitor/refresh", s.refreshHedgeMonitor)
	v1.POST("/hedge/run", s.runHedge)
	v1.POST("/hedge/exit", s.exitHedge)
	v1.POST("/hedge/enqueue", s.enqueueHedge)
	v1.GET("/audit-events", s.listAuditEvents)
}

func serveIndexHTML(c *gin.Context) {
	body, err := readIndexHTML()
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", body)
}

func readIndexHTML() ([]byte, error) {
	for _, path := range []string{"index.html", "../../index.html"} {
		body, err := os.ReadFile(path)
		if err == nil {
			return body, nil
		}
	}
	return nil, errors.New("index.html not found")
}

type createHedgeConfigRequest struct {
	ExchangeAccountID uint            `json:"exchange_account_id"`
	Source            string          `json:"source"`
	Symbol            string          `json:"symbol"`
	TargetSymbol      string          `json:"target_symbol"`
	TargetHedgeRatio  decimal.Decimal `json:"target_hedge_ratio"`
	FirstTriggerUSDT  decimal.Decimal `json:"first_trigger_usdt"`
	RebalanceUSDT     decimal.Decimal `json:"rebalance_usdt"`
	ExitUSDT          decimal.Decimal `json:"exit_usdt"`
	MaxSlippageBps    int             `json:"max_slippage_bps"`
	MaxNotionalUSDT   decimal.Decimal `json:"max_notional_usdt"`
	MinOrderUSDT      decimal.Decimal `json:"min_order_usdt"`
	Leverage          int             `json:"leverage"`
	Enabled           *bool           `json:"enabled"`
	DryRun            *bool           `json:"dry_run"`
	Metadata          json.RawMessage `json:"metadata"`
}

func (s *Server) createHedgeConfig(c *gin.Context) {
	var req createHedgeConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if req.ExchangeAccountID == 0 || req.Symbol == "" {
		respondError(c, http.StatusBadRequest, errors.New("exchange_account_id and symbol are required"))
		return
	}
	if len(req.Metadata) == 0 {
		req.Metadata = []byte("{}")
	}
	enabled := boolOrDefault(req.Enabled, true)
	dryRun := boolOrDefault(req.DryRun, false)

	config := mgmt.HedgeConfig{
		ExchangeAccountID: req.ExchangeAccountID,
		Source:            req.Source,
		Symbol:            req.Symbol,
		TargetSymbol:      req.TargetSymbol,
		TargetHedgeRatio:  req.TargetHedgeRatio,
		FirstTriggerUSDT:  req.FirstTriggerUSDT,
		RebalanceUSDT:     req.RebalanceUSDT,
		ExitUSDT:          req.ExitUSDT,
		MaxSlippageBps:    req.MaxSlippageBps,
		MaxNotionalUSDT:   req.MaxNotionalUSDT,
		MinOrderUSDT:      req.MinOrderUSDT,
		Leverage:          req.Leverage,
		Enabled:           enabled,
		DryRun:            dryRun,
		Metadata:          datatypes.JSON(req.Metadata),
	}
	if err := s.mgmt.CreateHedgeConfig(c.Request.Context(), &config); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusCreated, config)
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func (s *Server) listHedgeConfigs(c *gin.Context) {
	configs, err := s.mgmt.ListHedgeConfigs(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": configs})
}

func (s *Server) upsertExposure(c *gin.Context) {
	var req service.UpsertExposureInput
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	exposure, err := s.hedge.UpsertExposure(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, exposure)
}

func (s *Server) syncExposures(c *gin.Context) {
	if s.exposureSync == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("exposure sync is not configured"))
		return
	}
	result, err := s.exposureSync.SyncEnabledConfigs(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) syncPositions(c *gin.Context) {
	if s.positionSync == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("position sync is not configured"))
		return
	}
	result, err := s.positionSync.Sync(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) refreshHedgeMonitor(c *gin.Context) {
	if s.hedgeMonitor == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("hedge monitor is not configured"))
		return
	}
	result, err := s.hedgeMonitor.Refresh(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) listHedgeMonitor(c *gin.Context) {
	if s.hedgeMonitor == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("hedge monitor is not configured"))
		return
	}
	agentID, err := strconv.ParseUint(c.Query("agent_id"), 10, 64)
	if err != nil || agentID == 0 {
		respondError(c, http.StatusBadRequest, errors.New("valid agent_id is required"))
		return
	}
	result, err := s.hedgeMonitor.List(c.Request.Context(), agentID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) upsertPosition(c *gin.Context) {
	var req service.UpsertPositionInput
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	position, err := s.hedge.UpsertPosition(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, position)
}

func (s *Server) runHedge(c *gin.Context) {
	var req service.RunInput
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	result, err := s.hedge.RunOnce(c.Request.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, mgmt.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrRunLocked) {
			status = http.StatusConflict
		}
		respondError(c, status, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) exitHedge(c *gin.Context) {
	var req service.ExitInput
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	result, err := s.hedge.ExitHedge(c.Request.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, mgmt.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrRunLocked) {
			status = http.StatusConflict
		}
		respondError(c, status, err)
		return
	}
	responseStatus := http.StatusOK
	if result.CloseRequest != nil {
		switch result.CloseRequest.Status {
		case mgmt.HedgeCloseRequested, mgmt.HedgeCloseSubmitted, mgmt.HedgeCloseVerifying:
			responseStatus = http.StatusAccepted
		}
	}
	c.JSON(responseStatus, result)
}

func (s *Server) enqueueHedge(c *gin.Context) {
	var req service.RunInput
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if s.queue == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("async queue is not configured"))
		return
	}
	info, err := s.queue.EnqueueRunHedge(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"id":        info.ID,
		"queue":     info.Queue,
		"type":      info.Type,
		"state":     info.State.String(),
		"max_retry": info.MaxRetry,
	})
}

func (s *Server) listAuditEvents(c *gin.Context) {
	limit := int32(50)
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			respondError(c, http.StatusBadRequest, err)
			return
		}
		limit = int32(parsed)
	}
	events, err := s.hedge.ListAuditEvents(c.Request.Context(), limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": events})
}

func respondError(c *gin.Context, status int, err error) {
	c.JSON(status, gin.H{"error": err.Error()})
}

func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		observability.RecordHTTPRequest(
			c.Request.Method,
			path,
			strconv.Itoa(c.Writer.Status()),
			time.Since(start),
		)
	}
}
