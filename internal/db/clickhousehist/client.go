package clickhousehist

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const maxResponseBytes = 8 << 20

var databaseNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Config struct {
	Host           string
	HTTPPort       string
	Database       string
	Username       string
	Password       string
	ConnectTimeout time.Duration
	Timeout        time.Duration
}

type ExposureKey struct {
	AgentID uint64
	Symbol  string
}

type NetExposure struct {
	AgentID         uint64
	Symbol          string
	NetQuantity     decimal.Decimal
	PositionRows    int64
	SourceEventTime time.Time
}

type Client struct {
	endpoint   string
	username   string
	password   string
	httpClient *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	endpoint, err := buildEndpoint(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: cfg.ConnectTimeout}).DialContext

	return &Client{
		endpoint: endpoint,
		username: cfg.Username,
		password: cfg.Password,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
	}, nil
}

func (c *Client) ListNetExposures(ctx context.Context, keys []ExposureKey) (map[ExposureKey]NetExposure, error) {
	normalizedKeys := normalizeExposureKeys(keys)
	if len(normalizedKeys) == 0 {
		return map[ExposureKey]NetExposure{}, nil
	}

	query := buildNetExposureQuery(normalizedKeys)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(query))
	if err != nil {
		return nil, fmt.Errorf("create clickhouse request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query clickhouse current positions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return nil, fmt.Errorf("query clickhouse current positions: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return decodeNetExposureRows(io.LimitReader(resp.Body, maxResponseBytes))
}

func buildEndpoint(cfg Config) (string, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return "", errors.New("CLICKHOUSE_HISTORY_HOST is required")
	}
	database := strings.TrimSpace(cfg.Database)
	if database == "" {
		database = "freedex_history"
	}
	if !databaseNamePattern.MatchString(database) {
		return "", fmt.Errorf("invalid clickhouse database name %q", database)
	}

	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	parsed, err := url.Parse(host)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("invalid clickhouse host %q", cfg.Host)
	}
	if parsed.Port() == "" && strings.TrimSpace(cfg.HTTPPort) != "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), strings.TrimSpace(cfg.HTTPPort))
	}
	parsed.Path = "/"
	query := parsed.Query()
	query.Set("database", database)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func normalizeExposureKeys(keys []ExposureKey) []ExposureKey {
	seen := make(map[ExposureKey]struct{}, len(keys))
	result := make([]ExposureKey, 0, len(keys))
	for _, key := range keys {
		key.Symbol = normalizeSymbol(key.Symbol)
		if key.AgentID == 0 || key.Symbol == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].AgentID == result[j].AgentID {
			return result[i].Symbol < result[j].Symbol
		}
		return result[i].AgentID < result[j].AgentID
	})
	return result
}

func buildNetExposureQuery(keys []ExposureKey) string {
	tuples := make([]string, 0, len(keys))
	for _, key := range keys {
		tuples = append(tuples, fmt.Sprintf("(%d, '%s')", key.AgentID, escapeClickHouseString(key.Symbol)))
	}

	return fmt.Sprintf(`SELECT
    toString(toUInt64(agent_id)) AS agent_id,
    normalized_symbol AS symbol,
    toString(-sum(
        multiIf(
            upper(position_side) = 'SHORT', -toDecimal128OrZero(quantity, 18),
            upper(position_side) = 'LONG', toDecimal128OrZero(quantity, 18),
            toDecimal128OrZero(quantity, 18)
        )
    )) AS net_quantity,
    toString(count()) AS position_rows,
    toString(toUnixTimestamp64Milli(max(source_event_time))) AS source_event_time_ms
FROM (
    SELECT
        agent_id,
        upper(symbol) AS normalized_symbol,
        position_side,
        quantity,
        source_event_time
    FROM current_position_query
    WHERE status IN (1, 3)
)
WHERE (agent_id, normalized_symbol) IN (%s)
GROUP BY agent_id, normalized_symbol
FORMAT JSONEachRow`, strings.Join(tuples, ", "))
}

type netExposureRow struct {
	AgentID           string `json:"agent_id"`
	Symbol            string `json:"symbol"`
	NetQuantity       string `json:"net_quantity"`
	PositionRows      string `json:"position_rows"`
	SourceEventTimeMS string `json:"source_event_time_ms"`
}

func decodeNetExposureRows(reader io.Reader) (map[ExposureKey]NetExposure, error) {
	result := make(map[ExposureKey]NetExposure)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxResponseBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row netExposureRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("decode clickhouse exposure row: %w", err)
		}
		agentID, err := strconv.ParseUint(row.AgentID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse clickhouse agent id %q: %w", row.AgentID, err)
		}
		positionRows, err := strconv.ParseInt(row.PositionRows, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse clickhouse position rows for agent=%d symbol=%s: %w", agentID, row.Symbol, err)
		}
		sourceEventTimeMS, err := strconv.ParseInt(row.SourceEventTimeMS, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse clickhouse source event time for agent=%d symbol=%s: %w", agentID, row.Symbol, err)
		}
		quantity, err := decimal.NewFromString(row.NetQuantity)
		if err != nil {
			return nil, fmt.Errorf("parse clickhouse net quantity for agent=%d symbol=%s: %w", agentID, row.Symbol, err)
		}
		key := ExposureKey{AgentID: agentID, Symbol: normalizeSymbol(row.Symbol)}
		result[key] = NetExposure{
			AgentID:         key.AgentID,
			Symbol:          key.Symbol,
			NetQuantity:     quantity,
			PositionRows:    positionRows,
			SourceEventTime: unixMilli(sourceEventTimeMS),
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read clickhouse exposure rows: %w", err)
	}
	return result, nil
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func escapeClickHouseString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `'`, `\'`)
}

func unixMilli(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
