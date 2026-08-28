// Package api 是企微兼容层（/cgi-bin/*）与平台内部客户端接口。
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"im-/internal/store"
)

// writeErrcode 以企微错误结构应答。
func writeErrcode(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, map[string]any{"errcode": code, "errmsg": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON 解析请求体，失败时以企微错误结构应答。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErrcode(w, 40035, "invalid json") // 40035: 参数/JSON 格式错误
		return false
	}
	return true
}

// atoi64 解析 int64。
func atoi64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

// atoiDefault 解析整数，失败用默认值。
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// unixNow 返回当前秒级时间戳。
func unixNow() int64 { return time.Now().Unix() }

// serveMedia 输出素材文件内容。
func serveMedia(w http.ResponseWriter, st *store.Store, mediaID string) {
	m, err := st.GetMedia(mediaID)
	if err != nil {
		writeErrcode(w, 40007, "invalid media_id")
		return
	}
	http.ServeFile(w, newRequest(), m.FilePath)
}

func newRequest() *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	return r
}
