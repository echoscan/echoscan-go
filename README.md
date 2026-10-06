# `echoscan` (Go)

中文 | [English](#english)

## 中文

### 安装与最小调用

Go >=1.22。先在新客户目录运行 `go mod init example.com/echoscan-demo`，再安装。无第三方运行时依赖。

```bash
go get github.com/echoscan/echoscan-go@v0.2.4
```

本文安装示例固定为 0.2.4；下列客户端用法适用于该版本。

仅在服务端安全配置 `ECHOSCAN_LITE_KEY`；以下 Imprint 是虚构占位符，真实请求须替换为 Browser Verifier 返回的 Imprint。示例只输出风险状态，不记录完整报告、IP、Imprint、账号关系或 Secret。

```go
package main

import (
    "context"
    "fmt"
    "os"

    echoscan "github.com/echoscan/echoscan-go"
)

func main() {
    lite, err := echoscan.NewLiteClient(os.Getenv("ECHOSCAN_LITE_KEY"))
    if err != nil {
        fmt.Fprintln(os.Stderr, "invalid client configuration")
        os.Exit(1)
    }
    report, err := lite.GetReport(context.Background(), "imp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
    if err != nil {
        fmt.Fprintln(os.Stderr, "report request failed")
        os.Exit(1)
    }
    risk, ok := report["risk"].(map[string]any)
    if !ok {
        fmt.Fprintln(os.Stderr, "invalid report response")
        os.Exit(1)
    }
    fmt.Println("risk status:", risk["status"])
}
```

```bash
go run .
```

### Pro 方法、History 与 Account Map

Lite／Pro SDK 名称表示可调用的方法组合；基础 Report 合同一致。实际认证、Scope、额度和权限由服务端决定，不能由所选客户端类推断；套餐升级不意味着必须更换 Key。History 在 SDK 的 Pro 客户端提供，这不是 REST 端点一定拦截 Lite Key 的承诺。

在已有返回 error 的服务端函数中按需调用，复用最小示例的导入；不必为每次读取执行全部调用：

```go
pro, err := echoscan.NewProClient(os.Getenv("ECHOSCAN_PRO_KEY"))
if err != nil { return err }
ctx := context.Background()
imprint := "imp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
report, err := pro.GetReport(ctx, imprint)
if err != nil { return err }
accountReport, err := pro.GetReportWithOptions(ctx, imprint, echoscan.ReportOptions{AccountRef: "account_example"})
if err != nil { return err }
snapshot, err := pro.GetReport(ctx, imprint)
if err != nil { return err }
days, recent := 7, 20
history, err := pro.GetHistory(ctx, imprint, echoscan.HistoryQuery{Days: &days})
if err != nil { return err }
rangeHistory, err := pro.GetHistory(ctx, imprint, echoscan.HistoryQuery{From: "2026-09-01", To: "2026-09-07", Recent: &recent})
if err != nil { return err }
_, _, _, _, _ = report, accountReport, snapshot, history, rangeHistory
```

History 读取指定 Imprint 的访问历史，不创建账号关联。`days` 与 `from/to` 互斥，日期成对且为 `YYYY-MM-DD`，from 不晚于 to；days/recent 为正整数。日期范围和 recent 的服务端限制仍可能返回 400。

携带账号参数的调用发送 `POST /api/v1/fingerprint/report/{imprint}` 建立关联并返回 Report。账号参数为业务账号的非敏感引用（示例 `account_example`），须为 1–256 UTF-8 字节、无首尾空白或控制字符；不要发送邮箱、密码或 Secret。不携带参数的普通调用使用 `GET /api/v1/fingerprint/report/{imprint}`，只读取已存在的快照。

`GetReportWithOptions` 必须有合法 `AccountRef`，空值不会回退 GET。

POST 返回生成响应时已提交且可见的关系；不预先包含尚未完成的并发关联。稍后 GET 会重算该历史事件边界内的快照。`account_map` 可缺失，不能用缺失代替 false 或 0，也不能假造历史值。

### 正式 Report 与 History 返回

成功 JSON 由 SDK 透传。以下为生产者合法合成样例；Report 必有以下八个顶层字段，activity 与 account_map 可选。

```json
{
  "schema_version": "1.0",
  "imprint": "imp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "created_at": "2026-09-01T00:00:00Z",
  "device": {
    "id": "device-synthetic",
    "seen_before": false,
    "access_count": 1,
    "previous_seen_at": null
  },
  "risk": {
    "status": "PASS",
    "reasons": [],
    "findings": []
  },
  "browser": {
    "status": "PASS"
  },
  "operating_system": {
    "status": "PASS"
  },
  "network": {
    "status": "PASS",
    "ip_consistency": "UNKNOWN"
  }
}
```

`device.previous_seen_at` 必有且允许 null；`first_seen_at` 可缺失但不允许 null。browser／operating_system 的 name、version 可缺失。risk.reasons／findings 为固定数组，空数组有效；0、false 和空集合都是有效值，应保留。

可选 `activity` 包含固定 `5m`、`1h`、`24h` 窗口，每个窗口含整数 events、distinct_ips、distinct_countries。可选 `account_map` 包含 account_seen_before、relationship_seen_before（布尔）及 accounts_on_device、devices_on_account、accounts_first_seen_on_device_1h（整数），不是原始账号列表。两者缺失时均不允许伪造为全 0。

History 的合法空结果：

```json
{
  "imprint": "imp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "range": {
    "from": "2026-09-01",
    "to": "2026-09-01",
    "days": 1
  },
  "summary": {
    "events": 0,
    "truncated": false
  },
  "timeline": [],
  "recent": []
}
```

History 使用原有字段命名：summary.firstSeenAt／lastSeenAt 可缺失；timeline 条目含 date／count；recent 条目必有 at，可选 surface／sourceType／sourceHost。缺失历史数据不补造时间或来源。

字段权威：[开发者指南](https://echoscan.org/pages/developers/api-zh-CN.html)与[公开 OpenAPI 3.1](https://echoscan.org/openapi.json)，均可无需登录访问。Report 位于 `components.schemas["public.report.schema.json"]`，History 位于 `components.schemas["public.history.schema.json"]`。`getReport`／`bindAccountAndGetReport` 的 200 JSON 响应引用 Report；`getImprintHistory` 的 200 JSON 响应引用 History。

### 公开网络字段与 Geo 署名

`network.status` 与 `ip_consistency` 固定存在；一致性为 UNKNOWN／MATCH／MISMATCH。observed_ip、alternate_ip、country_code、location、provider、connection_type、asn、proxy_detected 可缺失，均不允许 null；location 内 country_name、region、city、timezone 也可缺失。缺少 geo 不代表已知为空，也不要在 SDK 自行推算定位。proxy_detected=false 表示已知的否定，区别于缺失。

若产品对外展示 MaxMind GeoLite 数据，请保留署名：

> This product includes GeoLite Data created by MaxMind, available from https://www.maxmind.com.

### 配置、错误与重试

| Environment | Default |
| --- | --- |
| `ECHOSCAN_SERVER_BASE_URL` | `https://api.echoscan.org` |
| `ECHOSCAN_SERVER_TIMEOUT_MS` | `5000` |
| `ECHOSCAN_SERVER_RETRIES` | `2` |

环境变量在客户端创建时读取；Key 仍为构造参数，上述 ECHOSCAN_LITE_KEY／PRO_KEY 只是调用方示例用名，不是 SDK 自动选择权限。

HTTP 错误体为 `{"error":{"code":"...","message":"..."}}`；SDK 将 HTTP 状态映射为归一化 code，原服务端 error.code 保留于下列细分码字段。比如 HTTP 409 account_ref_conflict 的 SDK code 是 unknown_error，不能把两者混为一谈。

`*APIError` 含 `Code`、`ServerCode`、`HTTPStatus`、`Message`、`RequestID`、`Retryable`；缺失细分码为 `""`。可用 `errors.As(err, &apiError)` 识别。

| HTTP | SDK code |
| --- | --- |
| 400 | `invalid_request` |
| 401 | `auth_failed` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 408 | `timeout` |
| 429 | `quota_exceeded` |
| 500 / 502 / 503 / 504 | `upstream_unavailable` |
| Other / 其他 | `unknown_error` |

当前 Go 环境解析只接受正整数，`ECHOSCAN_SERVER_RETRIES=0` 会回退到默认 2，不能通过此值关闭重试。每次 HTTP 请求超时，并同时受调用方 context 截止时间约束。

GET 默认最多 3 次尝试（首次＋2 次重试），在 HTTP 429、5xx 及捕获的网络失败时立即重试，没有退避或 Retry-After 等待。HTTP 408 不自动重试。网络错误归一化为 network_error 或 timeout。retryable 表示当前尝试还可自动重试；最终返回通常为 false，不证明问题永久不可恢复。

每次重试是独立请求；重复 Report／History GET 会经过服务端用量与限流策略，不能假设缓存命中或重复读取免费。不要无限轮询。Account Map POST 只尝试一次；超时后先核实已有关系／快照，再决定是否重发，不能按 retryable 或 GET 策略无条件重放。

---

## English

### Install and minimal call

Go >=1.22. Run `go mod init example.com/echoscan-demo` in a new customer directory before installing. No third-party runtime dependencies.

```bash
go get github.com/echoscan/echoscan-go@v0.2.4
```

These installation examples pin version 0.2.4; the client usage below targets that release.

Configure `ECHOSCAN_LITE_KEY` securely on the server. The imprint below is a synthetic placeholder; for a real request replace it with the imprint returned by Browser Verifier. The example prints only risk status, without logging a full report, IP, imprint, account relationships or secrets.

```go
package main

import (
    "context"
    "fmt"
    "os"

    echoscan "github.com/echoscan/echoscan-go"
)

func main() {
    lite, err := echoscan.NewLiteClient(os.Getenv("ECHOSCAN_LITE_KEY"))
    if err != nil {
        fmt.Fprintln(os.Stderr, "invalid client configuration")
        os.Exit(1)
    }
    report, err := lite.GetReport(context.Background(), "imp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
    if err != nil {
        fmt.Fprintln(os.Stderr, "report request failed")
        os.Exit(1)
    }
    risk, ok := report["risk"].(map[string]any)
    if !ok {
        fmt.Fprintln(os.Stderr, "invalid report response")
        os.Exit(1)
    }
    fmt.Println("risk status:", risk["status"])
}
```

```bash
go run .
```

### Pro methods, History and Account Map

Lite/Pro SDK names describe available method sets; the base Report contract is the same. The server determines authentication, scope, quota and permissions; the selected client class does not prove entitlement, and upgrading a plan does not imply that the key must change. History is exposed on the SDK Pro client, which does not promise REST plan rejection for a Lite key.

Call these selectively in an existing server function returning error, reusing the imports above; do not execute every call for each read:

```go
pro, err := echoscan.NewProClient(os.Getenv("ECHOSCAN_PRO_KEY"))
if err != nil { return err }
ctx := context.Background()
imprint := "imp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
report, err := pro.GetReport(ctx, imprint)
if err != nil { return err }
accountReport, err := pro.GetReportWithOptions(ctx, imprint, echoscan.ReportOptions{AccountRef: "account_example"})
if err != nil { return err }
snapshot, err := pro.GetReport(ctx, imprint)
if err != nil { return err }
days, recent := 7, 20
history, err := pro.GetHistory(ctx, imprint, echoscan.HistoryQuery{Days: &days})
if err != nil { return err }
rangeHistory, err := pro.GetHistory(ctx, imprint, echoscan.HistoryQuery{From: "2026-09-01", To: "2026-09-07", Recent: &recent})
if err != nil { return err }
_, _, _, _, _ = report, accountReport, snapshot, history, rangeHistory
```

History reads visits for an imprint and does not create account associations. `days` and `from/to` are mutually exclusive; dates must be paired, use `YYYY-MM-DD`, and from must not exceed to; days/recent are positive integers. Server limits on the range and recent can still return 400.

The account option sends `POST /api/v1/fingerprint/report/{imprint}` to bind a relationship and return a Report. Use a non-sensitive business account reference (synthetic `account_example` here), containing 1–256 UTF-8 bytes without leading/trailing whitespace or control characters; never send email, passwords or secrets. The ordinary call without the option uses `GET /api/v1/fingerprint/report/{imprint}` to read an existing snapshot.

`GetReportWithOptions` requires a valid `AccountRef`; an empty value does not fall back to GET.

POST reflects relationships committed and visible when the response is generated, without anticipating unfinished concurrent bindings. A later GET recomputes the snapshot within that historical event boundary. `account_map` may be absent; absence is not false or 0 and must not be filled with invented historical values.

### Canonical Report and History responses

The SDK forwards successful JSON. These are valid synthetic producer examples. Report requires the eight top-level fields shown; activity and account_map are optional.

```json
{
  "schema_version": "1.0",
  "imprint": "imp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "created_at": "2026-09-01T00:00:00Z",
  "device": {
    "id": "device-synthetic",
    "seen_before": false,
    "access_count": 1,
    "previous_seen_at": null
  },
  "risk": {
    "status": "PASS",
    "reasons": [],
    "findings": []
  },
  "browser": {
    "status": "PASS"
  },
  "operating_system": {
    "status": "PASS"
  },
  "network": {
    "status": "PASS",
    "ip_consistency": "UNKNOWN"
  }
}
```

`device.previous_seen_at` is required and nullable; `first_seen_at` is optional and not nullable. Browser/operating-system name and version may be absent. risk.reasons/findings are required arrays; empty arrays are valid. Preserve 0, false and empty collections as valid values.

Optional `activity` contains required `5m`, `1h`, `24h` windows, each with integer events, distinct_ips, distinct_countries. Optional `account_map` contains boolean account_seen_before, relationship_seen_before and integer accounts_on_device, devices_on_account, accounts_first_seen_on_device_1h, rather than raw account lists. Do not synthesize all-zero objects when either is absent.

A valid empty History result:

```json
{
  "imprint": "imp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "range": {
    "from": "2026-09-01",
    "to": "2026-09-01",
    "days": 1
  },
  "summary": {
    "events": 0,
    "truncated": false
  },
  "timeline": [],
  "recent": []
}
```

History retains its field naming: summary.firstSeenAt/lastSeenAt are optional; timeline entries carry date/count; recent entries require at and may carry surface/sourceType/sourceHost. Do not fabricate timestamps or sources for unavailable history.

Field authority: the [developer guide](https://echoscan.org/pages/developers/api-en-US.html) and [public OpenAPI 3.1](https://echoscan.org/openapi.json), both accessible without signing in. Report is at `components.schemas["public.report.schema.json"]`; History is at `components.schemas["public.history.schema.json"]`. The 200 JSON responses for `getReport` / `bindAccountAndGetReport` reference Report; the 200 JSON response for `getImprintHistory` references History.

### Public network fields and Geo attribution

`network.status` and `ip_consistency` are required; consistency is UNKNOWN/MATCH/MISMATCH. observed_ip, alternate_ip, country_code, location, provider, connection_type, asn, proxy_detected are optional and non-nullable; location.country_name/region/city/timezone are also optional. Missing geo is unavailable data; do not calculate a location in the SDK. proxy_detected=false is a known negative, distinct from absence.

When displaying MaxMind GeoLite data externally, retain attribution:

> This product includes GeoLite Data created by MaxMind, available from https://www.maxmind.com.

### Configuration, errors and retries

| Environment | Default |
| --- | --- |
| `ECHOSCAN_SERVER_BASE_URL` | `https://api.echoscan.org` |
| `ECHOSCAN_SERVER_TIMEOUT_MS` | `5000` |
| `ECHOSCAN_SERVER_RETRIES` | `2` |

Environment variables are read when the client is created. The key remains a constructor argument; ECHOSCAN_LITE_KEY/PRO_KEY are caller example names, not automatic SDK permission selection.

The HTTP error body is `{"error":{"code":"...","message":"..."}}`. SDK code is normalized from HTTP status, while the original error.code is retained in the server-code field below. For example HTTP 409 account_ref_conflict maps to SDK unknown_error; these are distinct codes.

`*APIError` carries `Code`, `ServerCode`, `HTTPStatus`, `Message`, `RequestID`, `Retryable`; a missing server code is `""`. Identify it with `errors.As(err, &apiError)`.

| HTTP | SDK code |
| --- | --- |
| 400 | `invalid_request` |
| 401 | `auth_failed` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 408 | `timeout` |
| 429 | `quota_exceeded` |
| 500 / 502 / 503 / 504 | `upstream_unavailable` |
| Other / 其他 | `unknown_error` |

The current Go environment parser accepts only positive integers: `ECHOSCAN_SERVER_RETRIES=0` falls back to 2 and does not disable retries. The HTTP timeout applies per request, also bounded by the caller context deadline.

GET defaults to at most 3 attempts (initial + 2 retries), retrying HTTP 429, 5xx and caught network failures immediately, without backoff or Retry-After waiting. HTTP 408 is not retried automatically. Network failures normalize to network_error or timeout. retryable reflects remaining automatic attempts; a final false value does not prove that a failure is permanent.

Each retry is a separate request. Repeated Report/History GET requests pass through server usage and rate-limit policy; do not assume cached or repeated reads are free, and avoid unlimited polling. Account Map POST is attempted once; after a timeout, check the existing relationship/snapshot before deciding to resend, rather than unconditionally replaying it using GET policy or retryable.
