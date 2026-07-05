# `echoscan` (Go)

中文 | [English](#english)

## 中文

### 安装

```bash
go get github.com/echoscan/echoscan-go@latest
```

### 导入

```go
import echoscan "github.com/echoscan/echoscan-go"
```

### 可用调用方式

- `echoscan.NewLiteClient(apiKey)`
- `echoscan.NewProClient(apiKey)`

方法：

- Lite: `GetReport(ctx, imprint)`
- Pro: `GetReport(ctx, imprint)`, `GetHistory(ctx, imprint, query)`


History 查询：

仅 Pro 客户端可用（需要 Pro API Key）。

- `HistoryQuery{ Days: &days }`
- `HistoryQuery{ From: "2026-03-01", To: "2026-03-18", Recent: &recent }`

### 返回值与错误

`GetReport()` 顶层结构：

```json
{
  "analysis": {},
  "lyingCount": 0,
  "projection": {}
}
```

`GetHistory()` 顶层结构：

```json
{
  "imprint": "fp_session_...",
  "range": {},
  "recent": [],
  "summary": {},
  "timeline": []
}
```

### Geo 字段说明（重要）

- SDK 只透传服务端返回，不在客户端自行做 IP 地理定位。
- 若服务端仅有 `GeoLite2-Country.mmdb`，通常只能返回国家级 geo；`region/city` 可能为空。
- 若服务端 geo enrichment 不可用，返回会降级（例如仅有 `country_hint` 或无 geo 字段）。
- 若服务端 geo enrichment 可用，主要查看 `projection.geo.data.geo_public`，字段包括：`country_code`、`country_name`、`region_code`、`region_name`、`city`、`timezone`。
- `analysis.geo` 提供主地理结果聚合字段：`final_ip`、`country_code`、`country_name`、`region_code`、`region_name`、`city`。
- `projection.geo.data` 还会带 `confidence` 与 `geo_low_trust`（例如 locale 与 IP 国家冲突）用于前端展示可信度提示。
- 在存储/调试字段中：`geo_source_ip` 是主判定 IP（canonical/best-effort），`geo_request_client_ip` 是请求链路观测 IP（证据）；两者可能不同（代理或多跳网络常见）。
- `confidence` 与 `geo_low_trust` 评价的是主结果（`geo_public` + `geo_source_ip`），不是 `geo_request_client_ip` 的评分。
- 若你的产品对外展示 MaxMind GeoLite 数据，请附署名：`This product includes GeoLite Data created by MaxMind, available from https://www.maxmind.com.`

完整字段说明链接：[echoscan](https://echoscan.org)

### 示例

```go
import (
    "context"
    "fmt"
    "os"

    echoscan "github.com/echoscan/echoscan-go"
)

ctx := context.Background()
pro, err := echoscan.NewProClient(os.Getenv("ECHOSCAN_PRO_KEY"))
if err != nil {
    panic(err)
}

report, err := pro.GetReport(ctx, "fp_session_123")
if err != nil {
    panic(err)
}

days := 7
history, err := pro.GetHistory(ctx, "fp_session_123", echoscan.HistoryQuery{Days: &days})
if err != nil {
    panic(err)
}

fmt.Println(report, history)
```

---

## English

### Install

```bash
go get github.com/echoscan/echoscan-go@latest
```

### Import

```go
import echoscan "github.com/echoscan/echoscan-go"
```

### Usage

- `echoscan.NewLiteClient(apiKey)`
- `echoscan.NewProClient(apiKey)`

Methods:

- Lite: `GetReport(ctx, imprint)`
- Pro: `GetReport(ctx, imprint)`, `GetHistory(ctx, imprint, query)`


History query:

Pro-only (requires a Pro API key).

- `HistoryQuery{ Days: &days }`
- `HistoryQuery{ From: "2026-03-01", To: "2026-03-18", Recent: &recent }`

### Response and errors

`GetReport()` top-level shape:

```json
{
  "analysis": {},
  "lyingCount": 0,
  "projection": {}
}
```

`GetHistory()` top-level shape:

```json
{
  "imprint": "fp_session_...",
  "range": {},
  "recent": [],
  "summary": {},
  "timeline": []
}
```

### Geo fields (important)

- The SDK forwards server responses and does not perform client-side IP geolocation.
- If the backend only has `GeoLite2-Country.mmdb`, geo is typically country-level only; `region/city` may be empty.
- If backend geo enrichment is unavailable, responses degrade (for example `country_hint` only, or no geo fields).
- When geo enrichment is available, the primary display payload is `projection.geo.data.geo_public` with fields: `country_code`, `country_name`, `region_code`, `region_name`, `city`, `timezone`.
- `analysis.geo` provides a flattened primary result: `final_ip`, `country_code`, `country_name`, `region_code`, `region_name`, `city`.
- `projection.geo.data` also includes `confidence` and `geo_low_trust` (for example locale vs IP-country mismatch) for trust-aware UX.
- In storage/debug fields, `geo_source_ip` is the primary canonical (best-effort) lookup IP, while `geo_request_client_ip` is request-chain observed evidence; they may differ (common with proxies or multi-hop networks).
- `confidence` and `geo_low_trust` rate the primary result (`geo_public` derived from `geo_source_ip`), not `geo_request_client_ip`.
- If your product displays MaxMind GeoLite data externally, include attribution: `This product includes GeoLite Data created by MaxMind, available from https://www.maxmind.com.`

Full field reference: [echoscan](https://echoscan.org)

### Example

```go
import (
    "context"
    "fmt"
    "os"

    echoscan "github.com/echoscan/echoscan-go"
)

ctx := context.Background()
pro, err := echoscan.NewProClient(os.Getenv("ECHOSCAN_PRO_KEY"))
if err != nil {
    panic(err)
}

report, err := pro.GetReport(ctx, "fp_session_123")
if err != nil {
    panic(err)
}

days := 7
history, err := pro.GetHistory(ctx, "fp_session_123", echoscan.HistoryQuery{Days: &days})
if err != nil {
    panic(err)
}

fmt.Println(report, history)
```
