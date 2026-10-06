package echoscan

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProGetReportWithOptionsPreservesGETAndPostsAccountRef(t *testing.T) {
	t.Helper()
	type observedRequest struct {
		method      string
		path        string
		apiKey      string
		contentType string
		userAgent   string
		requestID   string
		body        []byte
	}
	observed := make(chan observedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		observed <- observedRequest{
			method:      r.Method,
			path:        r.URL.Path,
			apiKey:      r.Header.Get("X-API-Key"),
			contentType: r.Header.Get("Content-Type"),
			userAgent:   r.Header.Get("User-Agent"),
			requestID:   r.Header.Get("X-Request-Id"),
			body:        body,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"account_map":{"accounts_on_device":2}}`))
	}))
	defer server.Close()
	t.Setenv("ECHOSCAN_SERVER_BASE_URL", server.URL)

	client, err := NewProClient("es_pro_secret")
	if err != nil {
		t.Fatalf("NewProClient: %v", err)
	}
	if _, err := client.GetReport(context.Background(), "fp_session_get"); err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	report, err := client.GetReportWithOptions(context.Background(), "fp_session_post", ReportOptions{AccountRef: "account_test_a"})
	if err != nil {
		t.Fatalf("GetReportWithOptions: %v", err)
	}
	if got := report["account_map"].(map[string]any)["accounts_on_device"]; got != float64(2) {
		t.Fatalf("accounts_on_device = %v", got)
	}

	getRequest := <-observed
	postRequest := <-observed
	if getRequest.method != http.MethodGet || len(getRequest.body) != 0 {
		t.Fatalf("old report request = %s %q", getRequest.method, getRequest.body)
	}
	if postRequest.method != http.MethodPost || postRequest.path != "/api/v1/fingerprint/report/fp_session_post" {
		t.Fatalf("account report request = %s %s", postRequest.method, postRequest.path)
	}
	if postRequest.apiKey != "es_pro_secret" || postRequest.contentType != "application/json" {
		t.Fatalf("account report headers = apiKey %q contentType %q", postRequest.apiKey, postRequest.contentType)
	}
	var body map[string]any
	if err := json.Unmarshal(postRequest.body, &body); err != nil {
		t.Fatalf("decode POST body: %v", err)
	}
	if len(body) != 1 || body["account_ref"] != "account_test_a" {
		t.Fatalf("POST body = %#v", body)
	}
	if strings.Contains(postRequest.userAgent, "account_test_a") || strings.Contains(postRequest.requestID, "account_test_a") {
		t.Fatal("accountRef leaked into request metadata")
	}
}

func TestProGetReportWithOptionsValidatesAccountRefLocally(t *testing.T) {
	var attempts atomic.Int32
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"account_map":{"accounts_on_device":1}}`))
	}))
	defer server.Close()
	t.Setenv("ECHOSCAN_SERVER_BASE_URL", server.URL)

	client, err := NewProClient("es_pro_secret")
	if err != nil {
		t.Fatalf("NewProClient: %v", err)
	}
	invalidAccountRefs := []string{
		"",
		"   ",
		" leading",
		"trailing ",
		strings.Repeat("a", 255) + "é",
		"bad\x00value",
		string([]byte{0xff}),
	}
	for _, accountRef := range invalidAccountRefs {
		_, err := client.GetReportWithOptions(context.Background(), "fp_session_invalid", ReportOptions{AccountRef: accountRef})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != ErrorInvalidRequest || apiErr.HTTPStatus != http.StatusBadRequest || apiErr.Retryable {
			t.Fatalf("accountRef %q error = %#v", accountRef, err)
		}
	}
	if attempts.Load() != 0 {
		t.Fatalf("invalid account refs made %d HTTP requests", attempts.Load())
	}

	_, err = client.GetReportWithOptions(context.Background(), "fp_session_unicode", ReportOptions{AccountRef: "账户-α-😀"})
	if err != nil {
		t.Fatalf("valid Unicode accountRef: %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("valid Unicode accountRef made %d HTTP requests", attempts.Load())
	}
	var body map[string]string
	if err := json.Unmarshal(receivedBody, &body); err != nil {
		t.Fatalf("decode Unicode POST body: %v", err)
	}
	if body["account_ref"] != "账户-α-😀" {
		t.Fatalf("Unicode POST body = %#v", body)
	}
}

func TestProAccountRefPostDoesNotRetryAndPreservesServerCode(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_conflict")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"account_ref_conflict","message":"conflict"}}`))
	}))
	defer server.Close()
	t.Setenv("ECHOSCAN_SERVER_BASE_URL", server.URL)
	t.Setenv("ECHOSCAN_SERVER_RETRIES", "5")

	client, err := NewProClient("es_pro_secret")
	if err != nil {
		t.Fatalf("NewProClient: %v", err)
	}
	_, err = client.GetReportWithOptions(context.Background(), "fp_session_conflict", ReportOptions{AccountRef: "account_test_b"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.Code != ErrorUnknownError || apiErr.ServerCode != "account_ref_conflict" || apiErr.HTTPStatus != http.StatusConflict {
		t.Fatalf("API error = %#v", apiErr)
	}
	if apiErr.Retryable || attempts.Load() != 1 {
		t.Fatalf("retryable = %v attempts = %d", apiErr.Retryable, attempts.Load())
	}
}

func TestProAccountRefPostDoesNotRetryServiceUnavailable(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"account_map_dependency_unavailable","message":"unavailable"}}`))
	}))
	defer server.Close()
	t.Setenv("ECHOSCAN_SERVER_BASE_URL", server.URL)
	t.Setenv("ECHOSCAN_SERVER_RETRIES", "5")

	client, err := NewProClient("es_pro_secret")
	if err != nil {
		t.Fatalf("NewProClient: %v", err)
	}
	_, err = client.GetReportWithOptions(context.Background(), "fp_session_unavailable", ReportOptions{AccountRef: "account_test_c"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.Code != ErrorUpstreamUnavailable || apiErr.ServerCode != "account_map_dependency_unavailable" {
		t.Fatalf("API error = %#v", apiErr)
	}
	if apiErr.Retryable || attempts.Load() != 1 {
		t.Fatalf("retryable = %v attempts = %d", apiErr.Retryable, attempts.Load())
	}
}

func identityScopeExamples(t *testing.T, kind string) []string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	file := filepath.Join(filepath.Dir(currentFile), "testdata", "identity-scope-examples.generated.json")
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var corpus map[string][]struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(contents, &corpus); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	examples := corpus[kind]
	if len(examples) == 0 {
		t.Fatalf("missing %s identity-scope contract examples", kind)
	}
	values := make([]string, 0, len(examples))
	for _, example := range examples {
		values = append(values, example.Value)
	}
	return values
}

func TestDebugClientFollowsContractScopeExamplesLocally(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		attempts.Add(1)
	}))
	defer server.Close()
	t.Setenv("ECHOSCAN_SERVER_BASE_URL", server.URL)

	if _, err := NewDebugClient("debug_secret"); err == nil {
		t.Fatal("legacy unscoped constructor must fail closed")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != ErrorInvalidRequest || apiErr.HTTPStatus != 400 || apiErr.Retryable || apiErr.RequestID != "" {
			t.Fatalf("legacy constructor error = %#v", err)
		}
	}
	namespaces := map[string]bool{}
	for _, scope := range identityScopeExamples(t, "valid") {
		if _, err := NewScopedDebugClient("debug_secret", scope); err != nil {
			t.Fatalf("valid contract scope %q: %v", scope, err)
		}
		namespace, _, _ := strings.Cut(scope, ":")
		namespaces[namespace] = true
	}
	for _, namespace := range []string{"site", "workspace", "internal"} {
		if !namespaces[namespace] {
			t.Fatalf("contract examples missing %s scope", namespace)
		}
	}
	for _, scope := range identityScopeExamples(t, "invalid") {
		if _, err := NewScopedDebugClient("debug_secret", scope); err == nil {
			t.Fatalf("scope %q must fail", scope)
		} else if strings.Contains(err.Error(), scope) && scope != "" {
			t.Fatal("invalid scope value leaked in local error")
		}
	}
	if attempts.Load() != 0 {
		t.Fatalf("invalid scope made %d HTTP requests", attempts.Load())
	}
}

func TestScopedDebugClientSendsCanonicalScopeExactlyOnce(t *testing.T) {
	requests := make(chan *http.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv("ECHOSCAN_SERVER_BASE_URL", server.URL)

	client, err := NewScopedDebugClient("debug_secret", " WORKSPACE:Customer-A ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetReport(context.Background(), "fp_session_report"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetDetails(context.Background(), "fp_session_details"); err != nil {
		t.Fatal(err)
	}
	for index, wantPath := range []string{"/api/v1/internal/fingerprint/report/fp_session_report", "/api/v1/internal/fingerprint/details/fp_session_details"} {
		req := <-requests
		if req.URL.Path != wantPath || req.URL.RawQuery != "identityScope=workspace%3Acustomer-a" {
			t.Fatalf("request %d URL = %s", index, req.URL.String())
		}
		if values := req.URL.Query()["identityScope"]; len(values) != 1 || values[0] != "workspace:customer-a" {
			t.Fatalf("request %d scope = %#v", index, values)
		}
	}
}

func TestScopedDebugClientMapsHTTPFailuresAndRetriesOnly5xx(t *testing.T) {
	for _, tc := range []struct {
		status       int
		wantCode     ErrorCode
		wantAttempts int32
	}{
		{http.StatusBadRequest, ErrorInvalidRequest, 1},
		{http.StatusNotFound, ErrorNotFound, 1},
		{http.StatusServiceUnavailable, ErrorUpstreamUnavailable, 3},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"code":"synthetic"}}`))
			}))
			defer server.Close()
			t.Setenv("ECHOSCAN_SERVER_BASE_URL", server.URL)
			t.Setenv("ECHOSCAN_SERVER_RETRIES", "2")
			client, err := NewScopedDebugClient("debug_secret", "workspace:synthetic")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetReport(context.Background(), "fp_session_failure")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != tc.wantCode || apiErr.HTTPStatus != tc.status || apiErr.Retryable || apiErr.RequestID == "" {
				t.Fatalf("error = %#v", err)
			}
			if attempts.Load() != tc.wantAttempts {
				t.Fatalf("attempts = %d want %d", attempts.Load(), tc.wantAttempts)
			}
		})
	}
}
