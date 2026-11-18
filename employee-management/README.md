# Employee Management System (Pure Go)

REST API quản lý nhân viên và phòng ban, viết hoàn toàn bằng thư viện chuẩn của Go
(`net/http`, `encoding/json`, `database/sql`, `context`, `sync`, `log/slog`, `testing`).
`go.mod` **không có dependency ngoài nào**.

## Cấu trúc thư mục

```
employee-management/
├── cmd/server/main.go           # Điểm vào: đọc config, nối các tầng, graceful shutdown
├── internal/
│   ├── config/                  # Đọc cấu hình từ biến môi trường
│   ├── model/                   # Domain types + lỗi dùng chung (ErrNotFound, ValidationError…)
│   ├── repository/
│   │   ├── repository.go        # Interface EmployeeRepository, DepartmentRepository
│   │   ├── memory/              # Mặc định: map + sync.RWMutex (thread-safe)
│   │   └── postgres/            # Tuỳ chọn: database/sql + câu lệnh JOIN thật
│   ├── service/                 # Business logic: validate, phân trang, soft/hard delete
│   ├── handler/                 # Tầng HTTP: parse request, trả JSON, router
│   ├── middleware/              # Logging, Recover (panic → 500), Timeout (context deadline)
│   └── worker/                  # Goroutine nền export nhân viên ra file JSON
└── go.mod
```

Chiều phụ thuộc chỉ đi một hướng: `handler → service → repository (interface) ← memory | postgres`.
`internal/` đảm bảo package bên ngoài module không import được các tầng này.

## Chạy thử

Yêu cầu **Go 1.22+** (dùng routing pattern `GET /employees/{id}` của `net/http` từ 1.22).
Cài trên Windows: `winget install GoLang.Go` (mở terminal mới sau khi cài).

```bash
cd employee-management
go run ./cmd/server
# time=... level=INFO msg="demo data loaded" departments=2 employees=4
# time=... level=INFO msg="http server listening" addr=:8080 storage=memory deleteMode=soft
```

Server tự nạp dữ liệu mẫu (2 phòng ban, 4 nhân viên) để bạn gọi API được ngay.
Dừng bằng `Ctrl+C`: server ngừng nhận request mới, chờ request đang chạy xong rồi mới thoát.

## API

| Method | Path | Mô tả | Thành công |
|---|---|---|---|
| POST | `/employees` | Thêm nhân viên | 201 + header `Location` |
| GET | `/employees?limit=10&offset=0` | Danh sách có phân trang | 200 |
| GET | `/employees/search?keyword=dev` | Tìm theo name hoặc position (contains, không phân biệt hoa thường) | 200 |
| GET | `/employees/{id}` | Chi tiết | 200 |
| PUT | `/employees/{id}` | Thay toàn bộ (bắt buộc đủ field) | 200 |
| PATCH | `/employees/{id}` | Cập nhật từng phần (chỉ field gửi lên) | 200 |
| DELETE | `/employees/{id}` | Xoá (soft/hard theo `DELETE_MODE`) | 204 |
| POST | `/departments` | Tạo phòng ban (tên là duy nhất, không phân biệt hoa thường) | 201 |
| GET | `/departments` | Tất cả phòng ban | 200 |
| GET | `/departments/{id}/employees` | Nhân viên của phòng ban (JOIN, kèm `departmentName`) | 200 |
| POST | `/exports` | Yêu cầu export ngay (chạy nền) | 202 |
| GET | `/exports/latest` | Kết quả lần export gần nhất | 200 |
| GET | `/healthz` | Health check | 200 |

Quy tắc validate: `name`, `position` bắt buộc, tối đa 100 ký tự; `age` từ 18 đến 65;
`salary` > 0; `departmentId` phải tồn tại. `limit` mặc định 10, tối đa 100.

Mọi lỗi có cùng một dạng JSON:

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "one or more fields are invalid",
    "details": { "age": "must be between 18 and 65", "departmentId": "department 9 does not exist" }
  }
}
```

| HTTP | `code` | Khi nào |
|---|---|---|
| 400 | `INVALID_JSON`, `INVALID_ID`, `INVALID_QUERY` | Request sai định dạng (JSON hỏng, field lạ, id không phải số dương…) |
| 400 | `VALIDATION_FAILED` | Dữ liệu sai quy tắc nghiệp vụ, `details` liệt kê từng field |
| 404 | `NOT_FOUND` | Không có bản ghi (kể cả bản ghi đã soft-delete) |
| 409 | `ALREADY_EXISTS` | Trùng tên phòng ban |
| 413 | `BODY_TOO_LARGE` | Body > 1 MiB |
| 503 | `TIMEOUT` | Xử lý quá `REQUEST_TIMEOUT` |
| 500 | `INTERNAL` | Lỗi hệ thống (chi tiết chỉ ghi vào log, không trả cho client) |

### Ví dụ với curl (Git Bash / Linux / macOS)

```bash
curl -X POST localhost:8080/departments -H "Content-Type: application/json" -d '{"name":"Marketing"}'

curl -X POST localhost:8080/employees -H "Content-Type: application/json" \
  -d '{"name":"John Doe","age":28,"position":"Backend Developer","departmentId":1,"salary":2000}'

curl "localhost:8080/employees?limit=2&offset=0"
curl "localhost:8080/employees/search?keyword=developer"
curl -X PATCH localhost:8080/employees/1 -H "Content-Type: application/json" -d '{"salary":2700}'
curl localhost:8080/departments/1/employees
curl -X DELETE -i localhost:8080/employees/1
curl -X POST localhost:8080/exports && curl localhost:8080/exports/latest
```

> **Tiếng Việt có dấu trên Windows:** khi gõ trực tiếp trong `-d '...'`, Windows có thể chuyển
> chuỗi sang code page của console (không phải UTF-8) và tên bị lỗi font. Hãy lưu body vào file
> UTF-8 rồi gửi bằng `--data-binary @body.json`.

## Cấu hình (biến môi trường)

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Địa chỉ lắng nghe |
| `REQUEST_TIMEOUT` | `5s` | Deadline của context cho mỗi request |
| `SHUTDOWN_TIMEOUT` | `10s` | Thời gian tối đa chờ request đang chạy khi tắt |
| `DELETE_MODE` | `soft` | `soft` = đánh dấu `deletedAt`, `hard` = xoá hẳn |
| `STORAGE` | `memory` | `memory` hoặc `postgres` |
| `DB_DRIVER` / `DB_DSN` | `pgx` / rỗng | Dùng khi `STORAGE=postgres` |
| `SEED_DATA` | `true` | Nạp dữ liệu mẫu (chỉ với `memory`) |
| `EXPORT_DIR` | `exports` | Thư mục ghi file export |
| `EXPORT_INTERVAL` | `5m` | Chu kỳ export tự động; `0` = tắt (vẫn export được qua `POST /exports`) |

```bash
DELETE_MODE=hard EXPORT_INTERVAL=30s go run ./cmd/server             # Git Bash
$env:DELETE_MODE="hard"; $env:EXPORT_INTERVAL="30s"; go run ./cmd/server  # PowerShell
```

## Các điểm thiết kế chính

**Phân lớp và interface.** Service chỉ biết `repository.EmployeeRepository` (interface), nên đổi
`memory` sang `postgres` không phải sửa một dòng business logic nào. Ngược lại, interface của
service được khai báo ngay trong package `handler` (nơi sử dụng, đúng idiom Go), nên test handler
có thể truyền vào một fake bất kỳ.

**Thread-safety.**
- `memory.Store` dùng một `sync.RWMutex` cho cả hai bảng: nhiều request đọc chạy song song, ghi thì
  độc quyền. Dữ liệu luôn được trả về dạng bản sao, caller không thể sửa trực tiếp vào store.
- Cập nhật dùng `Update(ctx, id, fn)`: đọc, sửa, ghi trong **cùng một lần giữ lock**. Nếu làm kiểu
  `Get` rồi `Save`, hai PATCH đồng thời sẽ ghi đè nhau. Test
  `TestEmployeeRepository_ConcurrentUpdatesAreAtomic` chạy 100 goroutine cùng tăng lương; bản cài
  đặt "Get rồi Save" sẽ làm mất hầu hết các lần cập nhật đó.
- Bản Postgres đạt cùng đảm bảo bằng `SELECT ... FOR UPDATE` trong transaction.

**Context xuyên suốt.** Middleware `Timeout` gắn deadline vào `r.Context()`; context đó đi qua
service xuống repository (`ctx.Err()` ở bản memory, `QueryContext` ở bản Postgres). Hết hạn thì trả
503 `TIMEOUT` (xem `TestTimeoutAndPanicRecovery`).

**Xử lý lỗi tập trung.** Handler trả `error` thay vì tự ghi response; `respondError` là nơi duy
nhất ánh xạ lỗi sang HTTP status (`errors.Is` / `errors.As`).

**PATCH và PUT.** `UpdateEmployeeInput` dùng field con trỏ: `nil` nghĩa là "không gửi, giữ
nguyên", phân biệt được với giá trị 0. PUT tái sử dụng chung luồng validate nhưng bắt buộc đủ field.

**Background worker.**
- Chỉ một goroutine chạy `Exporter.Run`, nhận việc từ `time.Ticker` (định kỳ) và channel `trigger`
  (theo yêu cầu), nên các lần export không bao giờ chồng nhau.
- Channel `trigger` có buffer 1: gọi `POST /exports` nhiều lần liên tiếp được gộp lại, vì lần export
  đang chờ sẽ đọc dữ liệu mới nhất khi bắt đầu.
- File được ghi vào file tạm rồi `os.Rename`, nên không ai đọc phải file ghi dở.
- Khi tắt server, lần export đang chạy được làm nốt (`context.WithoutCancel` + timeout riêng);
  `main` chờ nó bằng `sync.WaitGroup`.

## Test

```bash
go test ./...
go test -race ./...   # khuyến nghị; trên Windows cần CGO + gcc (vd. MSYS2 mingw-w64)
```

| Package | Nội dung test |
|---|---|
| `repository/memory` | Tạo đồng thời (ID duy nhất), cập nhật đồng thời không mất dữ liệu, phân trang và tìm kiếm, soft/hard delete, JOIN, context bị huỷ |
| `service` | Toàn bộ quy tắc validate, PATCH/PUT, chuẩn hoá phân trang, chọn soft/hard delete (dùng **mock** repository) |
| `handler` | End-to-end qua router bằng `httptest`: mã lỗi, phân trang, tìm kiếm, phòng ban, timeout, panic recovery |
| `worker` | Ghi file JSON, ghi nhận lỗi, gộp trigger, chạy theo lịch và theo yêu cầu |

## Dùng PostgreSQL (tuỳ chọn)

`database/sql` của thư viện chuẩn không đi kèm driver nào, nên bước này buộc phải thêm **một**
dependency ngoài (chỉ là driver, không phải ORM). Toàn bộ SQL nằm trong `internal/repository/postgres`.

1. `go get github.com/jackc/pgx/v5`
2. Tạo file `cmd/server/driver.go`:
   ```go
   package main

   import _ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
   ```
3. Chạy (bảng được tạo tự động từ `schema.sql` khi khởi động):
   ```bash
   STORAGE=postgres DB_DSN="postgres://user:pass@localhost:5432/ems?sslmode=disable" go run ./cmd/server
   ```
