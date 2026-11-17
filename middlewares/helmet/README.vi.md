# Middleware Helmet

*`helmet` đặt các HTTP security response header mà OWASP khuyến nghị, với mặc định an toàn phù hợp cho cả JSON API lẫn trang HTML render phía server.*

- [Middleware Helmet](#middleware-helmet)
  - [Tính Năng Chính](#tính-năng-chính)
  - [Cách Dùng](#cách-dùng)
  - [Header Và Mặc Định](#header-và-mặc-định)
  - [Hằng Số](#hằng-số)
  - [Struct `Helmet`](#struct-helmet)
  - [Phương Thức Của `Helmet`](#phương-thức-của-helmet)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Quy Tắc](#quy-tắc)
  - [Benchmark](#benchmark)

## Tính Năng Chính
- Content Security Policy chặt mặc định (chỉ `'self'`, không inline script attribute, không cho nhúng frame)
- `Strict-Transport-Security` chỉ qua HTTPS (RFC 6797 § 7.2), đọc qua `proxy.Scheme`
- Một quy tắc cho mọi field: `""` là mặc định, `helmet.Disabled` tắt header, giá trị khác được gửi đúng như viết
- `Permissions-Policy` mặc định từ chối các tính năng mạnh của trình duyệt
- Mọi giá trị header được dựng một lần khi khởi động; không cấp phát mỗi request

## Cách Dùng

```go
package main

import (
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/middlewares/helmet"
)

func main() {
	app := core.New()

	app.BindGlobalMiddlewares(helmet.Helmet{
		ContentSecurityPolicy: "default-src 'self'; img-src 'self' https: data:",
		FrameOptions:          helmet.Disabled,
	})

	app.Create(
		core.ModuleBuilder().Build(),
	)
}
```

## Header Và Mặc Định

| Header | Mặc định |
|---|---|
| `Content-Security-Policy` | `default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests` |
| `Cross-Origin-Embedder-Policy` | không gửi |
| `Cross-Origin-Opener-Policy` | `same-origin` |
| `Cross-Origin-Resource-Policy` | `same-origin` |
| `X-Frame-Options` | `DENY` |
| `Permissions-Policy` | `accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=(), browsing-topics=()` |
| `Referrer-Policy` | `no-referrer` |
| `Strict-Transport-Security` | `max-age=63072000; includeSubDomains`, chỉ qua HTTPS, không bao giờ `preload` mặc định |
| `X-Content-Type-Options` | `nosniff` (luôn gửi) |
| `X-XSS-Protection` | `0` (luôn gửi) |
| `X-DNS-Prefetch-Control` | `off` (luôn gửi) |
| `X-Permitted-Cross-Domain-Policies` | `none` (luôn gửi) |
| `Origin-Agent-Cluster` | `?1` (luôn gửi) |

`X-Download-Options` không còn được gửi (chỉ Internet Explorer đọc nó). Trang có `<style>` hoặc
`<script>` inline, hoặc asset trên CDN, phải tự đặt policy; phục vụ asset (kể cả CSS và JavaScript
của Owlky) từ chính origin của bạn thì mặc định dùng được ngay.

## Hằng Số

| Tên | Giá trị | Ý nghĩa |
|---|---|---|
| `Disabled` | `"-"` | Giá trị field tắt một header |

## Struct `Helmet`

Mọi field (`ContentSecurityPolicy`, `CrossOriginEmbedderPolicy`, `CrossOriginOpenerPolicy`,
`CrossOriginResourcePolicy`, `FrameOptions`, `PermissionsPolicy`, `ReferrerPolicy`,
`StrictTransportSecurity`) đều có Type `string`, Default `""` (mặc định ở bảng trên), Required
`false`, và cùng quy tắc: `""` gửi mặc định, `helmet.Disabled` không gửi, giá trị khác gửi đúng như
viết. Chỉ thêm `preload` vào `StrictTransportSecurity` sau khi đã đọc hstspreload.org.

## Phương Thức Của `Helmet`

### NewMiddleware
```go
func (instance Helmet) NewMiddleware() common.MiddlewareFn
```

Dựng mọi giá trị header một lần; framework gọi khi `Create()`. Panic với `invalid helmet: ...` cho
giá trị có ký tự điều khiển (xuống dòng, `NUL`, `DEL`, escape).

### Use
```go
func (instance Helmet) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)
```

Giúp `Helmet` là một `common.MiddlewareFn`. Chỉ dùng trong test; framework luôn gọi `NewMiddleware`.

## Quy Tắc
- `Strict-Transport-Security` chỉ được gửi khi `proxy.Scheme(r)` là `https`: kết nối TLS trực tiếp,
  hoặc `https` do `proxy` tin cậy chuyển tiếp. Client không tin cậy không thể tự nhận HTTPS.
- Các header luôn gửi có trên mọi response đi qua `helmet`, kể cả `404` hay `405`, kể cả khi mọi
  header cấu hình được đều `Disabled`.
- Header được đặt trước khi tầng kế tiếp chạy, nên middleware sau hoặc handler có thể ghi đè.
- `Header().Add` trên một header `helmet` đã đặt không bao giờ làm thay đổi response khác.
- Giá trị header được dùng chung giữa các response (dựng một lần lúc khởi động). Chỉ đổi bằng
  `Header().Set`, `Add` hoặc `Del`, không bao giờ ghi thẳng vào slice
  (`w.Header()["Content-Security-Policy"][0] = "…"`): làm vậy đổi mọi response khác và là data race.
- Request và context được chuyển tiếp nguyên vẹn, body không bao giờ bị đọc.

## Benchmark

Phụ thuộc máy; xem bảng trong [README.md](README.md#benchmarks).
