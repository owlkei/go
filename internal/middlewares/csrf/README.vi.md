# Middleware CSRF

> **Lưu ý (design 0022):** package này nay là internal. Ứng dụng cấu hình qua `AppOptions.CSRF` (`owlkei.CSRFOptions`) cùng `AppOptions.Origins` trong `owlkei.New(&owlkei.AppOptions{…})`; các ví dụ `owlkei.New()`, `BindGlobalMiddlewares(…)` và import `middlewares/…` bên dưới là API cũ đã bị gỡ. Bản cập nhật: [README.md](README.md) và [Configuration](../../../README.md#configuration).

*`csrf` từ chối các request thay đổi trạng thái (state-changing) gửi từ trình duyệt ở origin khác, dựa trên header Fetch Metadata `Sec-Fetch-Site` và header `Origin`: không token, không cookie, không đọc body.*

- [Middleware CSRF](#middleware-csrf)
  - [Tính Năng Chính](#tính-năng-chính)
  - [Cách Dùng](#cách-dùng)
  - [Cách Quyết Định](#cách-quyết-định)
  - [Struct `CSRF`](#struct-csrf)
    - [TrustedOrigins](#trustedorigins)
  - [Phương Thức Của `CSRF`](#phương-thức-của-csrf)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Quy Tắc](#quy-tắc)
  - [Giới Hạn](#giới-hạn)
  - [Benchmark](#benchmark)

## Tính Năng Chính
- Quyết định do `http.CrossOriginProtection` của thư viện chuẩn (Go 1.25+) đưa ra, được đội Go bảo trì
- Hoạt động cho cả form lẫn `fetch`: không cần thêm gì vào trang hay front end
- Không có token để lộ, không có cookie để bị cố định (fixation), không lưu gì trong request context
- Client không phải trình duyệt (curl, ứng dụng mobile, server) đi qua nguyên vẹn
- Sau một `proxy` tin cậy bật `IsHostForwarded`, `Origin` được so với host được chuyển tiếp (forwarded host)
- Không cấp phát bộ nhớ trên các đường đi thường gặp

## Cách Dùng

```go
package main

import (
	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/middlewares/csrf"
)

func main() {
	app := owlkei.New()

	app.BindGlobalMiddlewares(csrf.CSRF{
		TrustedOrigins: []string{"https://app.example.com"},
	})

	app.Create(
		owlkei.ModuleBuilder().Build(),
	)
}
```

Một `POST` mà trang trên `https://evil.example` khiến trình duyệt gửi đi sẽ nhận:

```json
{"code":403,"error":"Forbidden","message":"Cross-origin request refused"}
```

## Cách Quyết Định

Trình duyệt hiện đại gửi `Sec-Fetch-Site` trên mọi request; trình duyệt cũ gửi `Origin` trên các
request không an toàn. Client không phải trình duyệt không gửi cả hai.

| Method | `Sec-Fetch-Site` | `Origin` | Kết quả |
|---|---|---|---|
| `GET`, `HEAD`, `OPTIONS` | bất kỳ | bất kỳ | cho qua |
| khác | `same-origin` hoặc `none` | bất kỳ | cho qua |
| khác | giá trị khác (`cross-site`, `same-site`, bất kỳ) | bất kỳ | `Origin` thuộc `TrustedOrigins`: cho qua; nếu không `403` |
| khác | không có | không có | cho qua (không phải trình duyệt) |
| khác | không có | host bằng host của request | cho qua (trình duyệt trước 2023, hoặc HTTP thường) |
| khác | không có | host khác, `null` hoặc không parse được | `Origin` thuộc `TrustedOrigins`: cho qua; nếu không `403` |

## Struct `CSRF`

### TrustedOrigins
Type: `[]string`

Default: không có

Required: `false`

Các origin được phép gửi request không an toàn từ origin khác: một front end ở domain khác, hoặc
một identity provider post ngược về ứng dụng. Mỗi phần tử là `scheme://host[:port]`, tối đa 64.
Khớp chính xác: scheme, port hay subdomain khác đều không được tin cậy, và `null` không bao giờ.

```go
csrf.CSRF{TrustedOrigins: []string{"https://app.example.com", "https://idp.example.org:8443"}}
```

## Phương Thức Của `CSRF`

### NewMiddleware
```go
func (instance CSRF) NewMiddleware() owlkei.MiddlewareFn
```

Kiểm tra và biên dịch các option một lần; framework gọi nó khi `Create()`. Nó panic với
`invalid csrf: ...` cho một origin không phải `scheme://host[:port]` (`example.com`,
`https://example.com/path`, `*`, `//example.com`, có query hoặc fragment) và cho hơn 64 origin.

### Use
```go
func (instance CSRF) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next)
```

Giúp `CSRF` là một `owlkei.MiddlewareFn`. Nó biên dịch option ở mỗi lần gọi, nên chỉ dùng trong
test; framework luôn gọi `NewMiddleware`.

## Quy Tắc
- Mọi dòng của bảng trên đúng cho `POST`, `PUT`, `PATCH` và `DELETE`.
- `same-site` bị từ chối: một subdomain anh em (`https://sub.example.com`) là origin khác.
- Giá trị `Sec-Fetch-Site` được so chính xác (`Cross-Site` bị từ chối như `cross-site`).
- Với method không an toàn, một `Origin` dài hơn 256 byte bị từ chối trước khi parse, kể cả khi
  nó bằng host.
- Host và port của origin phải cùng bằng của request: `http://example.com:9090` bị từ chối trên
  `example.com:8080`.
- Sau một `proxy` tin cậy bật `IsHostForwarded` và có forwarded host, và khi không có `Sec-Fetch-Site`, host của origin được
  so với `proxy.Host(r)`. Không bind `proxy` thì `X-Forwarded-Host` và `Forwarded` không bao giờ
  được đọc.
- Không bao giờ set cookie hay header token, dù cho qua hay từ chối. Body không bao giờ bị đọc;
  request và context của nó được chuyển tiếp nguyên vẹn.
- Phản hồi từ chối là error response của tầng HTTP; nó không bao giờ lặp lại origin.

## Giới Hạn
- Thay đổi trạng thái sau một `GET` không được bảo vệ: hãy giữ `GET` an toàn.
- Client không phải trình duyệt luôn đi qua: `csrf` không phải là xác thực (authentication).
- Trường hợp duy nhất mà Fetch Metadata và `Origin` không phân biệt được, một trang HTTP thường post
  sang site HTTPS cùng host trên trình duyệt trước 2023, được HSTS của `helmet` chặn.

## Benchmark

Phụ thuộc máy; đo trên Intel i7-9750H, Go 1.27.4, khi viết README này. Xem bảng trong
[README.md](README.md#benchmarks).
