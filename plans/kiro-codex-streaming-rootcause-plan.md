# Kiro × Codex (/v1/responses) — Root Cause & Remediation Plan

> Phạm vi: provider Kiro (AWS CodeWhisperer / Amazon Q) khi dùng qua Codex (OpenAI Responses API, `POST /v1/responses`).
> Ngày: 2026-05-29.
>
> **Trạng thái triển khai:**
> - ✅ Phase 0 (hạ log TEMP-DEBUG), Phase 1 (idle read-timeout), Phase 2 (finalize stream khi lỗi) — đã merge ở commit `7108b4ec`.
> - ✅ Phase 3 (first-token timeout, mặc định tắt), Phase 4 (transient-5xx blackout 60s → 5s), Phase 5 (config-ize timeouts) — đã làm trong lần này, build + test xanh.
> - ⏸️ Phase 6 (bỏ double-convert cho Codex) — **hoãn có chủ đích** (re-architecture rủi ro cao, cần đo lại tương thích Codex; chỉ làm khi có yêu cầu rõ).

---

## 1. Tóm tắt (Executive Summary)

Kiro OAuth + executor đã hoạt động ổn cho `/v1/chat/completions` và `/v1/messages`, nhưng **Codex dùng `/v1/responses` thì gặp 2 lỗi**:

1. **Treo ~15 phút rồi trả `500`** (lặp lại nhiều lần). → **Root cause chính, P0.**
2. **Cụm `502` rất nhanh (10–30ms)** trong lúc agentic tool-calling. → Thứ cấp, P1.

Nguyên nhân gốc của (1): **đường đọc streaming của Kiro KHÔNG có read-deadline**. Hằng số `kiroStreamingReadTimeout = 300s` và `kiroFirstTokenTimeout = 15s` đã được **định nghĩa và đưa vào `defaultRetryConfig()` nhưng không bao giờ được áp dụng** vào việc đọc body. Khi upstream AWS treo giữa chừng (giữ kết nối mở, không gửi data, không đóng), `io.ReadFull` block tới khi OS TCP timeout (~15 phút). Stream không bao giờ phát `response.completed` → Codex treo.

Đối chiếu open-source `jwadow/kiro-gateway` cho thấy họ **đặt `read=STREAMING_READ_TIMEOUT (300s)` trên HTTP client streaming** → upstream treo sẽ fail nhanh ở 300s, không bao giờ treo 15 phút. Đây chính là mảnh ghép đang thiếu trong codebase này.

---

## 2. Bằng chứng từ logs (`logs/stdout.log`, 92k dòng)

### 2.1 Treo 15 phút → 500 (lặp lại)
```
07:11:54  500 |  5m19s | POST "/v1/responses"
08:35:07  500 | 15m1s  | POST "/v1/responses"
08:50:09  500 | 15m1s  | POST "/v1/responses"
09:34:20  500 | 15m1s  | POST "/v1/responses"
13:28:27  500 | 15m2s  | POST "/v1/responses"
13:44:24  500 | 15m1s  | POST "/v1/responses"
```
Duration cực kỳ đều (~15m1s) ⇒ là **TCP timeout của OS**, không phải timeout cấu hình của app.

Log lỗi tầng executor xác nhận đúng điểm chết — đọc prelude của AWS Event Stream:
```
kiro: streamToChannel error: event stream fatal: failed to read prelude:
      read tcp 192.168.1.59:56962->3.210.84.31:443: read: operation timed out
kiro: streamToChannel error: failed to read prelude: stream error ... INTERNAL_ERROR; received from peer
kiro: streamToChannel error: failed to read prelude: context canceled   # client tự bỏ
```

### 2.2 Cụm 502 nhanh (10–30ms) khi agentic tool-calling
```
01:06:05  502 | 12ms | /v1/responses
01:06:05  502 |  5ms | /v1/responses
... (hàng chục cái trong ~30 giây, xen kẽ vài 200 thành công)
```
Xảy ra ngay sau `meteringEvent` + `reasoning_effort="xhigh"` + tool-calling. Đây là lúc Codex bắn nhiều request song song để trả tool results.

### 2.3 Quan sát bổ trợ
- Phần lớn `/v1/responses` **thành công** (200) trong 5–35s; một số tới `2m14s` vẫn 200. ⇒ pipeline dịch Responses↔Kiro về cơ bản đúng; vấn đề là **độ bền của stream**, không phải sai format.
- Non-streaming không bị treo vì client của nó có timeout 120s (xem §3.3).

---

## 3. Phân tích Root Cause

### 3.1 Luồng xử lý hiện tại
- Request: `Responses → ChatCompletions → Kiro` (`chain.go` + `buildKiroPayloadForFormat` nhánh `openai-response`).
- Response: `Kiro AWS EventStream → Claude SSE → ChatCompletions → Responses events`.
- `chain.go` **tổng hợp `[DONE]`** khi thấy `finish_reason`, để framer phát `response.completed`.

### 3.2 Root cause #1 (P0) — Thiếu read-deadline khi đọc stream
File: `internal/runtime/executor/kiro_executor.go`

- Hằng số đã có nhưng **không được dùng**:
  - `kiroFirstTokenTimeout = 15 * time.Second` (dòng ~66)
  - `kiroStreamingReadTimeout = 300 * time.Second` (dòng ~68)
  - Đưa vào `defaultRetryConfig()` (`FirstTokenTmout`, `StreamReadTmout`) nhưng **không nơi nào gọi `SetReadDeadline` / áp timeout** (chỉ `codex_websockets_executor.go` dùng `SetReadDeadline`).
- HTTP client cho streaming tạo với **timeout = 0** (không giới hạn):
  ```go
  httpClient := newKiroHTTPClientWithPooling(ctx, e.cfg, auth, 0) // executeStreamWithRetry
  ```
- `streamToChannel` đọc bằng `bufio.Reader` → `readEventStreamMessage` → `io.ReadFull(reader, prelude)` **blocking, không deadline**.
- Vòng lặp có `select { case <-ctx.Done() }` ở đầu mỗi iteration, **nhưng `io.ReadFull` là blocking** nên check ctx chỉ có tác dụng khi *client* tự ngắt (lúc đó `http.NewRequestWithContext(ctx)` mới hủy read). Khi **client (Codex) vẫn chờ** mà **upstream treo**, không ai hủy được read.

**Hệ quả:** upstream stall → read treo tới TCP timeout (~15m) → stream không có `finish_reason` → không có `[DONE]` → không có `response.completed` → Codex treo trọn 15 phút rồi handler trả 500.

### 3.3 Vì sao non-streaming không bị
`executeWithRetry` tạo client `newKiroHTTPClientWithPooling(ctx, e.cfg, auth, 120*time.Second)` → có total timeout 120s. Chỉ **đường streaming** dùng timeout 0 nên mới hở.

### 3.4 Root cause #2 (P1) — Cụm 502 khi agentic + hiệu ứng cascade
- Khi 1 request stream fail nhanh (upstream 5xx tức thời, hoặc "all endpoints exhausted", hoặc token vừa vào cooldown), error được map về nhóm transient.
- `sdk/cliproxy/auth/conductor.go` (case `408, 500, 502, 503, 504`): đặt `NextRetryAfter = now + 1 phút` ("transient upstream error").
- Trong agentic, Codex bắn **nhiều request song song**; một lỗi transient có thể **sideline auth 1 phút** → các request song song còn lại fail nhanh theo → **cụm 502**.
- Đây là vấn đề về *chính sách xử lý lỗi/cooldown* dưới tải song song, không phải lỗi dịch format.

### 3.5 Root cause #3 (kiến trúc, nền tảng) — Stream không "tự kết thúc sạch" khi lỗi
Khi `streamToChannel` gặp lỗi đọc fatal **sau khi đã stream một phần**, nó gửi `out <- {Err}` rồi return **mà không phát** `message_delta(stop_reason)` + `message_stop`. Do đó `chain.go` không có `finish_reason` để tổng hợp `[DONE]`, và `response.completed` không bao giờ tới Codex. Đây là lý do *vì sao một lỗi upstream lại biến thành "treo"* thay vì "kết thúc có lỗi". Bất kể timeout, ta cần đảm bảo stream **luôn kết thúc bằng một sự kiện terminal** mà Codex hiểu.

---

## 4. Đối chiếu open-source

| Dự án | Cách làm Codex/stream | Bài học rút ra |
|---|---|---|
| **jwadow/kiro-gateway** (Python/FastAPI) | Đặt `httpx.Timeout(connect=30, read=STREAMING_READ_TIMEOUT=300, write=30, pool=30)` cho client streaming; có `_warn_timeout_configuration()`; **chỉ expose `/v1/chat/completions` + `/v1/messages`** (không có `/v1/responses` double-convert); release mới nhất **v2.3 = "Codex app & Errors Release"**; retry tường minh 403/429/5xx. | **`read`-timeout 300s là chốt chặn chống treo** (đúng giá trị hằng số mình đã có nhưng chưa dùng). Codex chạy ổn qua **chat/completions** — đơn giản hơn nhiều so với bridge Responses. |
| **decolua/9router** (Node/TS) | Dự án lớn, "smart 3-tier fallback", có thư mục `open-sse` xử lý SSE riêng; kết nối Codex tới nhiều provider. | Tách tầng SSE + **fallback nhiều tầng** giúp một upstream treo không làm hỏng phiên; đáng tham khảo cho chính sách lỗi/cooldown (RC#2). |
| **CLIProxyAPI-Extended** (mrsuperei/HALDRO — nguồn gốc port) | Cùng kiến trúc `streamToChannel` này. | Hằng số timeout được "bê" sang nhưng **mảnh wiring áp dụng timeout bị thiếu** → cần tự bổ sung. |

**Kết luận đối chiếu:** giải pháp tận gốc tối thiểu = **áp read-deadline 300s vào đường streaming Kiro** (giống kiro-gateway) + **đảm bảo stream luôn finalize**. Tùy chọn chiến lược dài hạn: cân nhắc cho Codex đi qua `/v1/chat/completions` để bỏ bớt lớp double-convert Responses.

---

## 5. Phương án xử lý tận gốc (theo giai đoạn)

### Phase 0 — Tắt noise log (dọn dẹp, P0, 5 phút)
- Có dòng `log.Infof("TEMP-DEBUG-KIRO-PAYLOAD: ... body=%s", ...)` in **toàn bộ payload ở mức Info** trong `executeWithRetry`. Hạ xuống `Debug` hoặc bỏ. Không liên quan treo nhưng làm log phình + lộ nội dung.

### Phase 1 — Idle read-timeout cho streaming (P0, fix chính)
**Mục tiêu:** upstream treo phải bị cắt trong `kiroStreamingReadTimeout` (300s) thay vì ~15 phút.

**Cách (tối thiểu, tự chứa trong goroutine stream của `executeStreamWithRetry`, dòng ~1430):**
- Bọc `resp.Body` bằng một reader reset "watchdog" trước mỗi `Read`; nếu quá `kiroStreamingReadTimeout` không có byte nào → watchdog `resp.Body.Close()` → `io.ReadFull` bung ra với lỗi → `streamToChannel` thoát.
- Phác thảo (minh hoạ, chưa áp dụng):
  ```go
  idle := time.AfterFunc(kiroStreamingReadTimeout, func() { _ = resp.Body.Close() })
  defer idle.Stop()
  bodyReader := &idleResetReader{r: resp.Body, timer: idle, d: kiroStreamingReadTimeout}
  e.streamToChannel(ctx, bodyReader, out, ...)
  // ...
  type idleResetReader struct { r io.Reader; timer *time.Timer; d time.Duration }
  func (x *idleResetReader) Read(p []byte) (int, error) { x.timer.Reset(x.d); return x.r.Read(p) }
  ```
- **Lý do chọn "đóng body" thay vì `SetReadDeadline`:** body của `net/http` không expose `net.Conn` để set deadline; đóng body là cách chuẩn, an toàn để hủy read đang block; cũng không phải bơm thêm `CancelFunc` xuống sâu.
- **Không dùng `http.Client.Timeout`** vì đó là total-timeout, sẽ cắt cả stream khoẻ nhưng dài (logs có request 2m14s hợp lệ).

### Phase 2 — Luôn finalize stream khi lỗi/timeout (P0, đi kèm Phase 1)
**Mục tiêu:** Codex luôn nhận sự kiện terminal, không bao giờ treo kể cả khi upstream chết giữa chừng.
- Trong `streamToChannel`, khi gặp read-error fatal (kể cả do watchdog đóng body) mà **đã mở message/đã stream content**: phát `message_delta(stop_reason="error" hoặc "end_turn")` + `message_stop` **trước khi** return, để `chain.go` sinh `[DONE]` → framer phát `response.completed`.
- Nếu **chưa stream gì** (lỗi ngay đầu): giữ nguyên đường trả error message (handler `writeResponsesStreamError` → `event: error`) — vì lúc này status code chưa commit, client nhận lỗi sạch.
- Cân nhắc phát thêm `response.incomplete`/`response.failed` cho đúng spec Responses (tùy mức độ Codex hỗ trợ); tối thiểu cần một terminal event.

### Phase 3 — First-token timeout có kiểm soát (P1)
- `kiroFirstTokenTimeout = 15s` **quá ngắn** cho thinking models (logs có response hợp lệ tới 2m14s; token đầu có thể tới muộn). 
- Phương án: dùng **first-byte timeout riêng, rộng hơn** (vd 60–90s) tách khỏi idle-timeout giữa các chunk (300s). Hoặc đơn giản **chỉ dùng idle 300s cho mọi read** (giống kiro-gateway) ở Phase 1 và **khoan bật** first-token timeout cho tới khi có số liệu. → Khuyến nghị: Phase 1 chỉ áp idle 300s; Phase 3 chỉ làm nếu thực sự cần.

### Phase 4 — Chính sách lỗi/cooldown dưới tải song song (P1, xử lý cụm 502)
- Rà soát ánh xạ lỗi Kiro → HTTP status: phân biệt rõ **lỗi transient/đáng retry** với **lỗi đẩy auth vào cooldown 1 phút**.
- Tránh để **1 lỗi transient sideline auth 1 phút** làm fail loạt request song song (hiệu ứng cascade ở §3.4). Cân nhắc:
  - Không cooldown auth cho lỗi 502/transient nếu chưa lặp lại N lần.
  - Trả `429 + Retry-After` cho quota/cooldown thay vì 502 (Codex backoff đúng hơn).
  - Tham khảo mô hình fallback nhiều tầng của 9router.

### Phase 5 — Cấu hình hoá timeout (P2)
- Expose trong `config.yaml`: `kiro-streaming-read-timeout`, (tùy chọn) `kiro-first-token-timeout`. Default giữ 300s / (tắt hoặc 60s). Thêm cảnh báo khi cấu hình quá thấp (giống `_warn_timeout_configuration`).

### Phase 6 — (Chiến lược, P3) Cân nhắc bỏ double-convert cho Codex
- `/v1/responses` hiện đi `Responses→ChatCompletions→Kiro→ChatCompletions→Responses` (đã có 6+ commit fix lặt vặt). 
- Đánh giá phương án cho Codex point sang `/v1/chat/completions` (như kiro-gateway) để giảm bề mặt lỗi. Đây là quyết định kiến trúc, **chỉ làm sau khi Phase 1–2 đã ổn định** và cần đo lại tính tương thích Codex.

---

## 6. Thứ tự ưu tiên & ước lượng

| Phase | Ưu tiên | Tác động | Rủi ro | Ước lượng |
|---|---|---|---|---|
| 0. Hạ log TEMP-DEBUG | P0 | Dọn log | Rất thấp | 5 phút |
| 1. Idle read-timeout 300s | **P0** | **Hết treo 15 phút** | Thấp–TB | 0.5 ngày |
| 2. Finalize stream khi lỗi | **P0** | Codex không treo, kết thúc sạch | TB | 0.5–1 ngày |
| 3. First-token timeout | P1 | Cắt sớm stall đầu luồng | TB (dễ cắt nhầm) | 0.5 ngày |
| 4. Cooldown/502 policy | P1 | Bớt cụm 502 khi agentic | TB | 1 ngày |
| 5. Cấu hình hoá | P2 | Vận hành linh hoạt | Thấp | 0.5 ngày |
| 6. Bỏ double-convert | P3 | Giảm nợ kỹ thuật | Cao | điều tra riêng |

**Lộ trình tối thiểu để "hết treo": Phase 1 + Phase 2.**

---

## 7. Kế hoạch kiểm thử & nghiệm thu

1. **Unit test (executor):**
   - Mô phỏng upstream gửi prelude rồi **ngừng gửi** → khẳng định stream kết thúc trong ~`kiroStreamingReadTimeout`, có phát `message_stop`/terminal, **không** treo.
   - Upstream đóng kết nối giữa chừng → có terminal event.
2. **Integration (Responses framer):** đầu vào là stream lỗi giữa chừng → đầu ra có `response.completed` hoặc `event: error` hợp lệ (Codex parse được).
3. **Thủ công với Codex:** chạy phiên agentic dài; xác nhận không còn `500 | ~15m`; cụm 502 giảm.
4. **Quan sát log sau deploy:** không còn dòng `failed to read prelude: ... operation timed out` kéo theo 15 phút; xuất hiện `kiro: stream idle for 5m0s, closing upstream connection` (mức Warn) khi thực sự stall.
5. **Build/test:** `go build ./...` và `go test ./internal/runtime/executor/...` phải xanh; dọn file tạm.

---

## 8. Rủi ro & biện pháp giảm thiểu

- **Cắt nhầm stream khoẻ nhưng chậm:** dùng idle-timeout (khoảng-cách-giữa-byte) 300s, **không** dùng total-timeout; healthy stream luôn có byte định kỳ (reasoning/metering events) nên không chạm ngưỡng.
- **Đóng body từ goroutine khác khi đang Read:** là pattern chuẩn của Go cho read-timeout; `Close` an toàn khi gọi đồng thời với `Read`; double-close trả lỗi vô hại (đã bỏ qua).
- **`time.AfterFunc` + `Reset`:** AfterFunc không dùng channel nên `Reset` an toàn; `defer idle.Stop()` dọn timer khi thoát.
- **Phase 2 fabricate terminal event:** chỉ phát khi đã có message mở; không che giấu lỗi (vẫn log Warn/Error đầy đủ) — đổi "treo" thành "kết thúc có thông báo".
- **Phase 4/6 rủi ro cao hơn:** tách PR riêng, có thể bật/tắt qua config, rollout từ từ.

---

## 9. Điểm chạm code (tham chiếu, KHÔNG sửa trong tài liệu này)

- `internal/runtime/executor/kiro_executor.go`
  - Hằng số `kiroFirstTokenTimeout` (~66), `kiroStreamingReadTimeout` (~68), `defaultRetryConfig()` (~120–134).
  - `executeStreamWithRetry`: client streaming timeout 0; goroutine stream (~1430); gọi `streamToChannel` (~1448).
  - `streamToChannel`: vòng đọc + `readEventStreamMessage` (`io.ReadFull` không deadline); nhánh phát `message_delta`/`message_stop` cuối luồng (đích Phase 2).
  - `executeWithRetry`: dòng log `TEMP-DEBUG-KIRO-PAYLOAD` (Phase 0); client non-stream 120s.
- `internal/translator/kiro/openai/responses/chain.go`: tổng hợp `[DONE]` theo `finish_reason`.
- `sdk/api/handlers/openai/openai_responses_handlers.go`: `handleStreamingResponse` / `forwardResponsesStream` / `writeResponsesStreamError`.
- `sdk/cliproxy/auth/conductor.go`: ánh xạ lỗi `408/500/502/503/504` → cooldown 1 phút (Phase 4).
