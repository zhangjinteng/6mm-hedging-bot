package clickhousehist

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func TestClientListsNetExposures(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "reader" || password != "secret" {
			t.Fatalf("unexpected basic auth %q %q", username, password)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		requestBody = string(body)
		_, _ = writer.Write([]byte("{\"agent_id\":\"1\",\"symbol\":\"BNBUSDT\",\"net_quantity\":\"-2.5\",\"position_rows\":\"3\",\"source_event_time_ms\":\"1788243790394\"}\n"))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Host:     server.URL,
		Database: "freedex_history",
		Username: "reader",
		Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.ListNetExposures(context.Background(), []ExposureKey{
		{AgentID: 1, Symbol: "bnbusdt"},
		{AgentID: 1, Symbol: "BNBUSDT"},
	})
	if err != nil {
		t.Fatal(err)
	}

	key := ExposureKey{AgentID: 1, Symbol: "BNBUSDT"}
	exposure, exists := result[key]
	if !exists {
		t.Fatalf("missing exposure for %+v", key)
	}
	if !exposure.NetQuantity.Equal(decimal.RequireFromString("-2.5")) {
		t.Fatalf("unexpected net quantity %s", exposure.NetQuantity)
	}
	if exposure.PositionRows != 3 || exposure.SourceEventTime.IsZero() {
		t.Fatalf("unexpected exposure %+v", exposure)
	}
	if strings.Count(requestBody, "(1, 'BNBUSDT')") != 1 {
		t.Fatalf("expected de-duplicated query, got %s", requestBody)
	}
	if !strings.Contains(requestBody, "status IN (1, 3)") {
		t.Fatalf("missing active position filter: %s", requestBody)
	}
}

func TestClientReturnsClickHouseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "query failed", http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := NewClient(Config{Host: server.URL, Database: "freedex_history"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListNetExposures(context.Background(), []ExposureKey{{AgentID: 1, Symbol: "BNBUSDT"}})
	if err == nil || !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected clickhouse status error, got %v", err)
	}
}
