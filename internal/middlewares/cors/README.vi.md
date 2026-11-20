# Middleware CORS

> **Lưu ý (design 0022):** package này nay là internal. Ứng dụng cấu hình qua `AppOptions.CORS` (`owlkei.CORSOptions`) cùng `AppOptions.Origins` trong `owlkei.New(&owlkei.AppOptions{…})`; các ví dụ `owlkei.New()`, `BindGlobalMiddlewares(…)` và import `middlewares/…` bên dưới là API cũ đã bị gỡ. Bản cập nhật: [README.md](README.md) và [Configuration](../../../README.md#configuration).

*`cors` cho phép các trang trình duyệt bạn liệt kê đọc API của bạn từ origin khác, theo [giao thức CORS của Fetch Standard](https://fetch.spec.whatwg.org/#http-cors-protocol): request thật, preflight `OPTIONS`, credentials và private network access.*

- [Middleware CORS](#middleware-cors)
  - [Tính Năng Chính](#tính-năng-chính)
  - [Cách Dùng](#cách-dùng)
  - [Struct `CORS`](#struct-cors)
    - [AllowOrigins](#alloworigins)
    - [AllowMethods](#allowmethods)
    - [AllowHeaders](#allowheaders)
    - [ExposeHeaders](#exposeheaders)
    - [MaxAge](#maxage)
    - [IsAllowCredentials](#isallowcredentials)
    - [IsAllowPrivateNetwork](#isallowprivatenetwork)
  - [Phương Thức Của `CORS`](#phương-thức-của-cors)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [Quy Tắc](#quy-tắc)
  - [Giới Hạn](#giới-hạn)
  - [Benchmark](#benchmark)

## Tính Năng Chính
- Không origin nào được phép cho đến khi bạn liệt kê; `*` không bao giờ đi với credentials; `null` bị từ chối
- Origin chính xác và mẫu `scheme://*.host[:port]`, không dùng regular expression
- `Vary: Origin` trên mọi response, để cache không trả câu trả lời của origin này cho origin khác
- Chỉ `OPTIONS` có `Origin` và `Access-Control-Request-Method` mới là preflight: route `OPTIONS` của bạn vẫn chạy
- `Access-Control-Request-Headers` của request không bao giờ bị phản hồi lại
- Từ chối handshake WebSocket từ origin không được liệt kê (cross-site WebSocket hijacking)
- Mọi giá trị header được dựng một lần khi khởi động; tối đa một lần cấp phát mỗi request

## Cách Dùng

```go
package main

import (
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/middlewares/cors"
)

func main() {
	app := owlkei.New()

	app.BindGlobalMiddlewares(cors.CORS{
		AllowOrigins:       []string{"https://app.example.com", "https://*.example.com"},
		ExposeHeaders:      []string{"X-Total-Count"},
		MaxAge:             time.Hour,
		IsAllowCredentials: true,
	})

	app.Create(
		owlkei.ModuleBuilder().Build(),
	)
}
```

## Struct `CORS`

### AllowOrigins
Type: `[]string`

Default: không có

Required: `true`

Các origin mà trang của chúng được đọc response, tối đa 256:

- `https://app.example.com`, `http://localhost:3000`, `https://[::1]:8080`: một origin chính xác,
  chữ thường, không path, không `/` ở cuối.
- `https://*.example.com`, `https://*.example.com:8443`: mọi subdomain của `example.com` ở mọi độ
  sâu, cùng scheme và port. Không gồm chính `example.com`.
- `*` đứng một mình: mọi origin, trả về `Access-Control-Allow-Origin: *`. Không dùng với
  `IsAllowCredentials`.

### AllowMethods
Type: `[]string`

Default: `GET, HEAD, PUT, PATCH, POST, DELETE`

Required: `false`

Các method mà preflight cho phép, tối đa 64 token RFC 9110.

### AllowHeaders
Type: `[]string`

Default: `Content-Type, Authorization`

Required: `false`

Các request header mà preflight cho phép, tối đa 64 token RFC 9110. Danh sách luôn là của bạn:
header trình duyệt xin không bao giờ được chép lại.

### ExposeHeaders
Type: `[]string`

Default: không có

Required: `false`

Các response header mà script của trang được đọc ngoài các header đã safelist, tối đa 64 token.

### MaxAge
Type: `time.Duration`

Default: `10 * time.Minute`

Required: `false`

Thời gian trình duyệt được cache một preflight, gửi theo giây nguyên. 600 giây được mọi trình duyệt
chấp nhận. `0` hoặc giá trị âm nghĩa là mặc định.

### IsAllowCredentials
Type: `bool`

Default: `false`

Required: `false`

Cho phép trang gửi cookie và HTTP authentication kèm request và đọc câu trả lời
(`Access-Control-Allow-Credentials: true`). Chỉ cho các origin được liệt kê.

### IsAllowPrivateNetwork
Type: `bool`

Default: `false`

Required: `false`

Trả `Access-Control-Allow-Private-Network: true` cho preflight có
`Access-Control-Request-Private-Network: true` (WICG Private Network Access).

## Phương Thức Của `CORS`

### NewMiddleware
```go
func (instance CORS) NewMiddleware() owlkei.MiddlewareFn
```

Kiểm tra và biên dịch option một lần; framework gọi khi `Create()`. Panic với
`invalid cors: AllowOrigins is empty` cho giá trị rỗng, và `invalid cors: ...` cho `*` đi cùng
phần tử khác hoặc với `IsAllowCredentials`, origin không phải `scheme://host[:port]` chữ thường hay
`scheme://*.host[:port]`, phần tử `null`, hơn 256 origin, hơn 64 method hay header, và tên không phải
token RFC 9110.

### Use
```go
func (instance CORS) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next)
```

Giúp `CORS` là một `owlkei.MiddlewareFn`. Chỉ dùng trong test; framework luôn gọi `NewMiddleware`.

## Quy Tắc
- Với một danh sách (không phải `*`), mọi response có `Vary: Origin`, thêm vào `Vary` sẵn có: kể cả
  khi không có header `Origin` và khi origin không khớp.
- Request không có `Origin` tới route mà không có CORS header.
- **Request thật, origin khớp:** `Access-Control-Allow-Origin` là origin của request (`*` với `*`),
  thêm `Access-Control-Allow-Credentials: true` nếu bật credentials và
  `Access-Control-Expose-Headers` nếu có; rồi route chạy.
- **Request thật, origin khác:** không có CORS header, route vẫn chạy. CORS không phải kiểm soát
  truy cập.
- **Preflight** (`OPTIONS` có `Origin` và `Access-Control-Request-Method`), origin khớp: các header
  trên cùng `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers`, `Access-Control-Max-Age`,
  status `204`, `Content-Length: 0`; route không chạy.
- **Preflight, origin khác:** `204` chỉ với `Vary`.
- Mọi `OPTIONS` khác là request thật và tới route `PREFLIGHT_…` của bạn.
- Phần wildcard chỉ gồm `a-z`, `0-9`, `-` và `.`; `Origin` dài hơn 256 byte không khớp gì.
- **WebSocket** (`Upgrade: websocket`): `Origin` không khớp bị từ chối `403`, message
  `Origin not allowed`; khớp hoặc không có thì đi tiếp. Truyền `cors` cho `EnableWS` để kiểm tra
  handshake.
- Giá trị header được dùng chung giữa các response (dựng một lần lúc khởi động). Chỉ đổi bằng
  `Header().Set`, `Add` hoặc `Del`, không bao giờ ghi thẳng vào slice
  (`w.Header()["Access-Control-Allow-Methods"][0] = "…"`): làm vậy đổi mọi response khác và là data race.
- Request và context được chuyển tiếp nguyên vẹn, body không bao giờ bị đọc.

## Giới Hạn
- CORS chỉ kiểm soát việc trang trình duyệt được đọc gì. Nó không chặn request, và client không
  phải trình duyệt bỏ qua nó. Hãy bảo vệ thay đổi trạng thái bằng `csrf` và guard.

## Benchmark

Phụ thuộc máy; xem bảng trong [README.md](README.md#benchmarks).
