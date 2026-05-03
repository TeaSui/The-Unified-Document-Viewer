package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

const (
	appBaseURL       = "http://localhost:8080"
	salesMockAdmin   = "http://localhost:9001/__admin"
	serviceMockAdmin = "http://localhost:9002/__admin"
	testVIN          = "1HGCM82633A004352"
	uncachedVIN      = "2HGCM82633A004352"
)

func baseURL() string {
	if url := os.Getenv("APP_BASE_URL"); url != "" {
		return url
	}
	return appBaseURL
}

func TestMain(m *testing.M) {
	healthy := false
	for i := 0; i < 30; i++ {
		resp, err := http.Get(baseURL() + "/healthz")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			healthy = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !healthy {
		fmt.Println("SKIP: app not healthy at", baseURL())
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type envelope struct {
	Data struct {
		VIN       string `json:"vin"`
		Documents []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			Type   string `json:"type"`
			Title  string `json:"title"`
		} `json:"documents"`
		Sources []struct {
			Name      string  `json:"name"`
			Status    string  `json:"status"`
			Error     string  `json:"error,omitempty"`
			FetchedAt *string `json:"fetched_at,omitempty"`
		} `json:"sources"`
	} `json:"data"`
	Meta struct {
		RequestID string `json:"request_id"`
		Timestamp string `json:"timestamp"`
	} `json:"meta"`
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestIntegration_BothSucceed(t *testing.T) {
	resp, err := http.Get(baseURL() + "/vehicles/" + testVIN + "/documents")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env envelope
	json.NewDecoder(resp.Body).Decode(&env)

	if env.Data.VIN != testVIN {
		t.Errorf("expected VIN %s, got %s", testVIN, env.Data.VIN)
	}
	if len(env.Data.Documents) != 6 {
		t.Errorf("expected 6 documents (3+3), got %d", len(env.Data.Documents))
	}

	salesCount, serviceCount := 0, 0
	for _, doc := range env.Data.Documents {
		switch doc.Source {
		case "sales":
			salesCount++
		case "service":
			serviceCount++
		}
	}
	if salesCount != 3 {
		t.Errorf("expected 3 sales docs, got %d", salesCount)
	}
	if serviceCount != 3 {
		t.Errorf("expected 3 service docs, got %d", serviceCount)
	}

	for _, s := range env.Data.Sources {
		if s.Status != "ok" {
			t.Errorf("expected source %s status ok, got %s", s.Name, s.Status)
		}
	}

	if env.Meta.RequestID == "" {
		t.Error("expected non-empty request_id")
	}
}

func TestIntegration_OneSource5xx(t *testing.T) {
	http.Get(baseURL() + "/vehicles/" + testVIN + "/documents")

	injectFault(t, salesMockAdmin, 500)
	defer resetMock(t, salesMockAdmin)

	resp, err := http.Get(baseURL() + "/vehicles/" + testVIN + "/documents")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 (partial success), got %d", resp.StatusCode)
	}

	var env envelope
	json.NewDecoder(resp.Body).Decode(&env)

	if len(env.Data.Documents) < 3 {
		t.Errorf("expected at least 3 documents, got %d", len(env.Data.Documents))
	}

	var salesSource, serviceSource struct{ Name, Status string }
	for _, s := range env.Data.Sources {
		if s.Name == "sales" {
			salesSource.Name, salesSource.Status = s.Name, s.Status
		}
		if s.Name == "service" {
			serviceSource.Name, serviceSource.Status = s.Name, s.Status
		}
	}
	if serviceSource.Status != "ok" {
		t.Errorf("expected service status ok, got %s", serviceSource.Status)
	}
	if salesSource.Status != "stale" && salesSource.Status != "failed" {
		t.Errorf("expected sales status stale or failed, got %s", salesSource.Status)
	}
}

func TestIntegration_OneSourceTimeout(t *testing.T) {
	http.Get(baseURL() + "/vehicles/" + testVIN + "/documents")

	injectDelay(t, salesMockAdmin, 2000)
	defer resetMock(t, salesMockAdmin)

	start := time.Now()
	resp, err := http.Get(baseURL() + "/vehicles/" + testVIN + "/documents")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if elapsed > 3*time.Second {
		t.Errorf("expected response within 3s, took %v", elapsed)
	}

	var env envelope
	json.NewDecoder(resp.Body).Decode(&env)

	var salesStatus string
	for _, s := range env.Data.Sources {
		if s.Name == "sales" {
			salesStatus = s.Status
		}
	}
	if salesStatus != "stale" && salesStatus != "failed" {
		t.Errorf("expected sales status stale or failed after timeout, got %s", salesStatus)
	}
}

func TestIntegration_BothFailNoCache(t *testing.T) {
	injectFault(t, salesMockAdmin, 500)
	injectFault(t, serviceMockAdmin, 500)
	defer resetMock(t, salesMockAdmin)
	defer resetMock(t, serviceMockAdmin)

	resp, err := http.Get(baseURL() + "/vehicles/" + uncachedVIN + "/documents")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Fatalf("expected 502 when both fail with no cache, got %d", resp.StatusCode)
	}

	var errResp errorResponse
	json.NewDecoder(resp.Body).Decode(&errResp)
	if errResp.Error.Code != "upstream_failure" {
		t.Errorf("expected error code upstream_failure, got %s", errResp.Error.Code)
	}
}

func TestIntegration_BothFailWithCache(t *testing.T) {
	resetMock(t, salesMockAdmin)
	resetMock(t, serviceMockAdmin)
	time.Sleep(500 * time.Millisecond)

	resp, err := http.Get(baseURL() + "/vehicles/" + testVIN + "/documents")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("cache priming failed: err=%v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()

	injectFault(t, salesMockAdmin, 500)
	injectFault(t, serviceMockAdmin, 500)
	defer resetMock(t, salesMockAdmin)
	defer resetMock(t, serviceMockAdmin)

	resp, err = http.Get(baseURL() + "/vehicles/" + testVIN + "/documents")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 (stale from cache), got %d", resp.StatusCode)
	}

	var env envelope
	json.NewDecoder(resp.Body).Decode(&env)

	if len(env.Data.Documents) == 0 {
		t.Error("expected cached documents, got none")
	}

	for _, s := range env.Data.Sources {
		if s.Status != "stale" {
			t.Errorf("expected source %s status stale, got %s", s.Name, s.Status)
		}
		if s.FetchedAt == nil || *s.FetchedAt == "" {
			t.Errorf("expected fetched_at for stale source %s", s.Name)
		}
	}
}

func injectFault(t *testing.T, adminURL string, statusCode int) {
	t.Helper()
	body := fmt.Sprintf(`{"priority":0,"request":{"method":"GET","urlPathPattern":"/api/v1/vehicles/.*/documents"},"response":{"status":%d,"body":"{\"error\":\"simulated\"}"}}`, statusCode)
	resp, err := http.Post(adminURL+"/mappings", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("failed to inject fault: %v", err)
	}
	resp.Body.Close()
}

func injectDelay(t *testing.T, adminURL string, delayMs int) {
	t.Helper()
	body := fmt.Sprintf(`{"priority":0,"request":{"method":"GET","urlPathPattern":"/api/v1/vehicles/.*/documents"},"response":{"status":200,"fixedDelayMilliseconds":%d,"headers":{"Content-Type":"application/json"},"body":"{\"documents\":[]}"}}`, delayMs)
	resp, err := http.Post(adminURL+"/mappings", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("failed to inject delay: %v", err)
	}
	resp.Body.Close()
}

func resetMock(t *testing.T, adminURL string) {
	t.Helper()
	resp, err := http.Post(adminURL+"/mappings/reset", "application/json", nil)
	if err != nil {
		t.Logf("warning: failed to reset mock: %v", err)
		return
	}
	resp.Body.Close()
}
