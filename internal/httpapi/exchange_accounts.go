package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"

	"github.com/zhangjinteng/6mm-hedging-bot/internal/db/mgmt"
)

type createExchangeAccountRequest struct {
	Exchange            string          `json:"exchange"`
	Name                string          `json:"name"`
	CCXTID              string          `json:"ccxt_id"`
	MarketType          string          `json:"market_type"`
	Sandbox             *bool           `json:"sandbox"`
	DefaultSettle       string          `json:"default_settle"`
	AccountType         string          `json:"account_type"`
	ProductType         string          `json:"product_type"`
	Category            string          `json:"category"`
	PositionMode        string          `json:"position_mode"`
	MarginMode          string          `json:"margin_mode"`
	RecvWindowMS        int             `json:"recv_window_ms"`
	RateLimitMS         int             `json:"rate_limit_ms"`
	AllowedSymbols      []string        `json:"allowed_symbols"`
	APIKeyEncrypted     string          `json:"api_key_encrypted"`
	APIKeyHint          string          `json:"api_key_hint"`
	APISecretEncrypted  string          `json:"api_secret_encrypted"`
	PassphraseEncrypted string          `json:"passphrase_encrypted"`
	Status              string          `json:"status"`
	IsPrimary           bool            `json:"is_primary"`
	Metadata            json.RawMessage `json:"metadata"`
}

type updateExchangeAccountRequest struct {
	Exchange            *string          `json:"exchange"`
	Name                *string          `json:"name"`
	CCXTID              *string          `json:"ccxt_id"`
	MarketType          *string          `json:"market_type"`
	Sandbox             *bool            `json:"sandbox"`
	DefaultSettle       *string          `json:"default_settle"`
	AccountType         *string          `json:"account_type"`
	ProductType         *string          `json:"product_type"`
	Category            *string          `json:"category"`
	PositionMode        *string          `json:"position_mode"`
	MarginMode          *string          `json:"margin_mode"`
	RecvWindowMS        *int             `json:"recv_window_ms"`
	RateLimitMS         *int             `json:"rate_limit_ms"`
	AllowedSymbols      *[]string        `json:"allowed_symbols"`
	APIKeyEncrypted     *string          `json:"api_key_encrypted"`
	APIKeyHint          *string          `json:"api_key_hint"`
	APISecretEncrypted  *string          `json:"api_secret_encrypted"`
	PassphraseEncrypted *string          `json:"passphrase_encrypted"`
	Status              *string          `json:"status"`
	IsPrimary           *bool            `json:"is_primary"`
	Metadata            *json.RawMessage `json:"metadata"`
}

type exchangeAccountResponse struct {
	ID                 uint            `json:"id"`
	Exchange           string          `json:"exchange"`
	Name               string          `json:"name"`
	CCXTID             string          `json:"ccxt_id"`
	MarketType         string          `json:"market_type"`
	Sandbox            bool            `json:"sandbox"`
	DefaultSettle      string          `json:"default_settle"`
	AccountType        string          `json:"account_type"`
	ProductType        string          `json:"product_type"`
	Category           string          `json:"category"`
	PositionMode       string          `json:"position_mode"`
	MarginMode         string          `json:"margin_mode"`
	RecvWindowMS       int             `json:"recv_window_ms"`
	RateLimitMS        int             `json:"rate_limit_ms"`
	AllowedSymbols     json.RawMessage `json:"allowed_symbols"`
	APIKeySet          bool            `json:"api_key_set"`
	APISecretSet       bool            `json:"api_secret_set"`
	PassphraseSet      bool            `json:"passphrase_set"`
	PassphraseRequired bool            `json:"passphrase_required"`
	APIKeyHint         string          `json:"api_key_hint"`
	CredentialStatus   string          `json:"credential_status"`
	Status             string          `json:"status"`
	IsPrimary          bool            `json:"is_primary"`
	Metadata           json.RawMessage `json:"metadata"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

func (s *Server) createExchangeAccount(c *gin.Context) {
	var req createExchangeAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	account, err := req.toModel()
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if err := s.mgmt.CreateExchangeAccount(c.Request.Context(), &account); err != nil {
		respondManagementError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toExchangeAccountResponse(account))
}

func (s *Server) listExchangeAccounts(c *gin.Context) {
	accounts, err := s.mgmt.ListExchangeAccounts(c.Request.Context())
	if err != nil {
		respondManagementError(c, err)
		return
	}

	items := make([]exchangeAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, toExchangeAccountResponse(account))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) listExchangeAccountOptions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": mgmt.SupportedExchangeDefaults()})
}

func (s *Server) getExchangeAccount(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	account, err := s.mgmt.GetExchangeAccount(c.Request.Context(), id)
	if err != nil {
		respondManagementError(c, err)
		return
	}
	c.JSON(http.StatusOK, toExchangeAccountResponse(account))
}

func (s *Server) updateExchangeAccount(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	var req updateExchangeAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}

	patch, err := req.toPatch()
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	account, err := s.mgmt.UpdateExchangeAccount(c.Request.Context(), id, patch)
	if err != nil {
		respondManagementError(c, err)
		return
	}
	c.JSON(http.StatusOK, toExchangeAccountResponse(account))
}

func (s *Server) deleteExchangeAccount(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	if err := s.mgmt.DeleteExchangeAccount(c.Request.Context(), id); err != nil {
		respondManagementError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) setPrimaryExchangeAccount(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	account, err := s.mgmt.SetPrimaryExchangeAccount(c.Request.Context(), id)
	if err != nil {
		respondManagementError(c, err)
		return
	}
	c.JSON(http.StatusOK, toExchangeAccountResponse(account))
}

func (req createExchangeAccountRequest) toModel() (mgmt.ExchangeAccount, error) {
	allowedSymbols, err := mgmt.BuildAllowedSymbolsJSON(req.AllowedSymbols)
	if err != nil {
		return mgmt.ExchangeAccount{}, err
	}
	metadata, err := normalizeMetadata(req.Metadata)
	if err != nil {
		return mgmt.ExchangeAccount{}, err
	}

	sandbox := true
	if req.Sandbox != nil {
		sandbox = *req.Sandbox
	}

	account := mgmt.ExchangeAccount{
		Exchange:            req.Exchange,
		Name:                req.Name,
		CCXTID:              req.CCXTID,
		MarketType:          req.MarketType,
		Sandbox:             sandbox,
		DefaultSettle:       req.DefaultSettle,
		AccountType:         req.AccountType,
		ProductType:         req.ProductType,
		Category:            req.Category,
		PositionMode:        req.PositionMode,
		MarginMode:          req.MarginMode,
		RecvWindowMS:        req.RecvWindowMS,
		RateLimitMS:         req.RateLimitMS,
		AllowedSymbols:      allowedSymbols,
		APIKeyEncrypted:     req.APIKeyEncrypted,
		APIKeyHint:          req.APIKeyHint,
		APISecretEncrypted:  req.APISecretEncrypted,
		PassphraseEncrypted: req.PassphraseEncrypted,
		Status:              req.Status,
		IsPrimary:           req.IsPrimary,
		Metadata:            metadata,
	}
	if err := mgmt.PrepareExchangeAccountForSave(&account); err != nil {
		return mgmt.ExchangeAccount{}, err
	}
	return account, nil
}

func (req updateExchangeAccountRequest) toPatch() (mgmt.ExchangeAccountPatch, error) {
	patch := mgmt.ExchangeAccountPatch{
		Exchange:            req.Exchange,
		Name:                req.Name,
		CCXTID:              req.CCXTID,
		MarketType:          req.MarketType,
		Sandbox:             req.Sandbox,
		DefaultSettle:       req.DefaultSettle,
		AccountType:         req.AccountType,
		ProductType:         req.ProductType,
		Category:            req.Category,
		PositionMode:        req.PositionMode,
		MarginMode:          req.MarginMode,
		RecvWindowMS:        req.RecvWindowMS,
		RateLimitMS:         req.RateLimitMS,
		APIKeyEncrypted:     req.APIKeyEncrypted,
		APIKeyHint:          req.APIKeyHint,
		APISecretEncrypted:  req.APISecretEncrypted,
		PassphraseEncrypted: req.PassphraseEncrypted,
		Status:              req.Status,
		IsPrimary:           req.IsPrimary,
	}
	if req.AllowedSymbols != nil {
		allowedSymbols, err := mgmt.BuildAllowedSymbolsJSON(*req.AllowedSymbols)
		if err != nil {
			return mgmt.ExchangeAccountPatch{}, err
		}
		patch.AllowedSymbols = &allowedSymbols
	}
	if req.Metadata != nil {
		metadata, err := normalizeMetadata(*req.Metadata)
		if err != nil {
			return mgmt.ExchangeAccountPatch{}, err
		}
		patch.Metadata = &metadata
	}
	return patch, nil
}

func toExchangeAccountResponse(account mgmt.ExchangeAccount) exchangeAccountResponse {
	allowedSymbols := json.RawMessage(account.AllowedSymbols)
	if len(allowedSymbols) == 0 {
		allowedSymbols = json.RawMessage(`[]`)
	}
	metadata := json.RawMessage(account.Metadata)
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	return exchangeAccountResponse{
		ID:                 account.ID,
		Exchange:           account.Exchange,
		Name:               account.Name,
		CCXTID:             account.CCXTID,
		MarketType:         account.MarketType,
		Sandbox:            account.Sandbox,
		DefaultSettle:      account.DefaultSettle,
		AccountType:        account.AccountType,
		ProductType:        account.ProductType,
		Category:           account.Category,
		PositionMode:       account.PositionMode,
		MarginMode:         account.MarginMode,
		RecvWindowMS:       account.RecvWindowMS,
		RateLimitMS:        account.RateLimitMS,
		AllowedSymbols:     allowedSymbols,
		APIKeySet:          account.APIKeyEncrypted != "",
		APISecretSet:       account.APISecretEncrypted != "",
		PassphraseSet:      account.PassphraseEncrypted != "",
		PassphraseRequired: mgmt.PassphraseRequired(account),
		APIKeyHint:         account.APIKeyHint,
		CredentialStatus:   mgmt.CredentialStatus(account),
		Status:             account.Status,
		IsPrimary:          account.IsPrimary,
		Metadata:           metadata,
		CreatedAt:          account.CreatedAt,
		UpdatedAt:          account.UpdatedAt,
	}
}

func normalizeMetadata(raw json.RawMessage) (datatypes.JSON, error) {
	if len(raw) == 0 {
		return datatypes.JSON([]byte("{}")), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, errors.New("metadata must be a JSON object")
	}
	return datatypes.JSON(raw), nil
}

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	raw := c.Param(name)
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		respondError(c, http.StatusBadRequest, errors.New("invalid "+name))
		return 0, false
	}
	return uint(id), true
}

func respondManagementError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, mgmt.ErrNotFound):
		respondError(c, http.StatusNotFound, err)
	case errors.Is(err, mgmt.ErrInvalidConfig):
		respondError(c, http.StatusBadRequest, err)
	case strings.Contains(err.Error(), "duplicate key"):
		respondError(c, http.StatusConflict, err)
	default:
		respondError(c, http.StatusInternalServerError, err)
	}
}
