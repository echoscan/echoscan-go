package echoscan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://api.echoscan.org"
	defaultTimeoutMs = 5000
	defaultRetries   = 2
	sdkUserAgent     = "echoscan-go/0.2.2"
)

type ErrorCode string

const (
	ErrorInvalidRequest      ErrorCode = "invalid_request"
	ErrorAuthFailed          ErrorCode = "auth_failed"
	ErrorForbidden           ErrorCode = "forbidden"
	ErrorNotFound            ErrorCode = "not_found"
	ErrorQuotaExceeded       ErrorCode = "quota_exceeded"
	ErrorTimeout             ErrorCode = "timeout"
	ErrorUpstreamUnavailable ErrorCode = "upstream_unavailable"
	ErrorNetworkError        ErrorCode = "network_error"
	ErrorUnknownError        ErrorCode = "unknown_error"
)

type APIError struct {
	Code       ErrorCode
	HTTPStatus int
	Message    string
	RequestID  string
	Retryable  bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

type HistoryQuery struct {
	Days   *int
	From   string
	To     string
	Recent *int
}

type runtimeConfig struct {
	baseURL   string
	timeoutMs int
	retries   int
}

type baseClient struct {
	httpClient *http.Client
	baseURL    string
	retries    int
	apiKey     string
}

type LiteClient struct {
	base *baseClient
}

type ProClient struct {
	base *baseClient
}

type DebugClient struct {
	base *baseClient
}

func NewLiteClient(apiKey string) (*LiteClient, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, &APIError{
			Code:       ErrorInvalidRequest,
			HTTPStatus: 400,
			Message:    "apiKey is required for lite client",
			Retryable:  false,
		}
	}
	base, err := newBaseClient(key)
	if err != nil {
		return nil, err
	}
	return &LiteClient{base: base}, nil
}

func NewProClient(apiKey string) (*ProClient, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, &APIError{
			Code:       ErrorInvalidRequest,
			HTTPStatus: 400,
			Message:    "apiKey is required for pro client",
			Retryable:  false,
		}
	}
	base, err := newBaseClient(key)
	if err != nil {
		return nil, err
	}
	return &ProClient{base: base}, nil
}

func NewDebugClient(apiKey string) (*DebugClient, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, &APIError{
			Code:       ErrorInvalidRequest,
			HTTPStatus: 400,
			Message:    "apiKey is required for debug client",
			Retryable:  false,
		}
	}
	base, err := newBaseClient(key)
	if err != nil {
		return nil, err
	}
	return &DebugClient{base: base}, nil
}

func (c *LiteClient) GetReport(ctx context.Context, imprint string) (map[string]any, error) {
	im, err := validateImprint(imprint)
	if err != nil {
		return nil, err
	}
	return c.base.getJSON(ctx, "/api/v1/fingerprint/report-lite/"+url.PathEscape(im))
}

func (c *ProClient) GetReport(ctx context.Context, imprint string) (map[string]any, error) {
	im, err := validateImprint(imprint)
	if err != nil {
		return nil, err
	}
	return c.base.getJSON(ctx, "/api/v1/fingerprint/report/"+url.PathEscape(im))
}

func (c *ProClient) GetHistory(ctx context.Context, imprint string, query HistoryQuery) (map[string]any, error) {
	im, err := validateImprint(imprint)
	if err != nil {
		return nil, err
	}
	queryString, err := buildHistoryQuery(query)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/fingerprint/imprint/" + url.PathEscape(im) + "/history" + queryString
	return c.base.getJSON(ctx, path)
}

func (c *DebugClient) GetReport(ctx context.Context, imprint string) (map[string]any, error) {
	im, err := validateImprint(imprint)
	if err != nil {
		return nil, err
	}
	return c.base.getJSON(ctx, "/api/v1/internal/fingerprint/report/"+url.PathEscape(im))
}

func (c *DebugClient) GetDetails(ctx context.Context, imprint string) (map[string]any, error) {
	im, err := validateImprint(imprint)
	if err != nil {
		return nil, err
	}
	return c.base.getJSON(ctx, "/api/v1/internal/fingerprint/details/"+url.PathEscape(im))
}

func newBaseClient(apiKey string) (*baseClient, error) {
	cfg := buildRuntimeConfig()
	return &baseClient{
		httpClient: &http.Client{Timeout: time.Duration(cfg.timeoutMs) * time.Millisecond},
		baseURL:    cfg.baseURL,
		retries:    cfg.retries,
		apiKey:     apiKey,
	}, nil
}

func (c *baseClient) getJSON(ctx context.Context, path string) (map[string]any, error) {
	u := c.baseURL + path
	maxAttempts := c.retries + 1
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		requestID := generateRequestID()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, wrapAPIError(ErrorUnknownError, 0, "Unexpected error", requestID, false)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", sdkUserAgent)
		req.Header.Set("X-Request-Id", requestID)
		if c.apiKey != "" {
			req.Header.Set("X-API-Key", c.apiKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			retryable := attempt < maxAttempts
			code := ErrorNetworkError
			status := 0
			msg := "Network error"
			if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
				code = ErrorTimeout
				status = 408
				msg = "Request timed out"
			}
			mapped := wrapAPIError(code, status, msg, requestID, retryable)
			if retryable {
				lastErr = mapped
				continue
			}
			return nil, mapped
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		var payload map[string]any
		_ = json.Unmarshal(body, &payload)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if payload == nil {
				return map[string]any{}, nil
			}
			return payload, nil
		}

		code := mapStatusToCode(resp.StatusCode)
		retryable := attempt < maxAttempts && (resp.StatusCode >= 500 || resp.StatusCode == 429)
		responseRequestID := strings.TrimSpace(resp.Header.Get("X-Request-Id"))
		if responseRequestID == "" {
			responseRequestID = requestID
		}
		mapped := wrapAPIError(code, resp.StatusCode, defaultMessage(code), responseRequestID, retryable)
		if retryable {
			lastErr = mapped
			continue
		}
		return nil, mapped
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, wrapAPIError(ErrorUnknownError, 0, "Unexpected error", "", false)
}

func buildRuntimeConfig() runtimeConfig {
	baseURL := strings.TrimSpace(os.Getenv("ECHOSCAN_SERVER_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	timeoutMs := parseIntEnv("ECHOSCAN_SERVER_TIMEOUT_MS", defaultTimeoutMs)
	retries := parseIntEnv("ECHOSCAN_SERVER_RETRIES", defaultRetries)
	if retries < 0 {
		retries = defaultRetries
	}

	return runtimeConfig{
		baseURL:   baseURL,
		timeoutMs: timeoutMs,
		retries:   retries,
	}
}

func parseIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func validateImprint(imprint string) (string, error) {
	im := strings.TrimSpace(imprint)
	if im == "" {
		return "", &APIError{
			Code:       ErrorInvalidRequest,
			HTTPStatus: 400,
			Message:    "imprint must be a non-empty string",
			Retryable:  false,
		}
	}
	return im, nil
}

func buildHistoryQuery(q HistoryQuery) (string, error) {
	hasDays := q.Days != nil
	hasFrom := strings.TrimSpace(q.From) != ""
	hasTo := strings.TrimSpace(q.To) != ""

	if hasDays && (hasFrom || hasTo) {
		return "", &APIError{
			Code:       ErrorInvalidRequest,
			HTTPStatus: 400,
			Message:    "history query: days and from/to are mutually exclusive",
			Retryable:  false,
		}
	}
	if hasFrom != hasTo {
		return "", &APIError{
			Code:       ErrorInvalidRequest,
			HTTPStatus: 400,
			Message:    "history query: from and to must be provided together",
			Retryable:  false,
		}
	}

	values := url.Values{}
	if hasDays {
		if *q.Days <= 0 {
			return "", &APIError{
				Code:       ErrorInvalidRequest,
				HTTPStatus: 400,
				Message:    "history query: days must be a positive number",
				Retryable:  false,
			}
		}
		values.Set("days", strconv.Itoa(*q.Days))
	}
	if hasFrom && hasTo {
		if !isYYYYMMDD(q.From) {
			return "", &APIError{
				Code:       ErrorInvalidRequest,
				HTTPStatus: 400,
				Message:    "from must use YYYY-MM-DD format",
				Retryable:  false,
			}
		}
		if !isYYYYMMDD(q.To) {
			return "", &APIError{
				Code:       ErrorInvalidRequest,
				HTTPStatus: 400,
				Message:    "to must use YYYY-MM-DD format",
				Retryable:  false,
			}
		}
		if q.From > q.To {
			return "", &APIError{
				Code:       ErrorInvalidRequest,
				HTTPStatus: 400,
				Message:    "history query: from must be <= to",
				Retryable:  false,
			}
		}
		values.Set("from", q.From)
		values.Set("to", q.To)
	}
	if q.Recent != nil {
		if *q.Recent <= 0 {
			return "", &APIError{
				Code:       ErrorInvalidRequest,
				HTTPStatus: 400,
				Message:    "history query: recent must be a positive number",
				Retryable:  false,
			}
		}
		values.Set("recent", strconv.Itoa(*q.Recent))
	}

	encoded := values.Encode()
	if encoded == "" {
		return "", nil
	}
	return "?" + encoded, nil
}

func isYYYYMMDD(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func mapStatusToCode(status int) ErrorCode {
	switch status {
	case 400:
		return ErrorInvalidRequest
	case 401:
		return ErrorAuthFailed
	case 403:
		return ErrorForbidden
	case 404:
		return ErrorNotFound
	case 408:
		return ErrorTimeout
	case 429:
		return ErrorQuotaExceeded
	case 500, 502, 503, 504:
		return ErrorUpstreamUnavailable
	default:
		return ErrorUnknownError
	}
}

func defaultMessage(code ErrorCode) string {
	switch code {
	case ErrorInvalidRequest:
		return "Invalid request"
	case ErrorAuthFailed:
		return "Authentication failed"
	case ErrorForbidden:
		return "Forbidden"
	case ErrorNotFound:
		return "Not found"
	case ErrorQuotaExceeded:
		return "Quota exceeded"
	case ErrorTimeout:
		return "Request timed out"
	case ErrorUpstreamUnavailable:
		return "Upstream service unavailable"
	case ErrorNetworkError:
		return "Network error"
	default:
		return "Unexpected error"
	}
}

func wrapAPIError(code ErrorCode, status int, message, requestID string, retryable bool) *APIError {
	return &APIError{
		Code:       code,
		HTTPStatus: status,
		Message:    message,
		RequestID:  requestID,
		Retryable:  retryable,
	}
}

func generateRequestID() string {
	return fmt.Sprintf("req_%d_%06d", time.Now().UnixNano(), rand.Intn(1_000_000))
}
