package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"im-/internal/store"
)

// 素材有效期对齐企微：临时素材 3 天。
const mediaTTL = 3 * 24 * time.Hour

// maxUploadBytes 单文件上限（企微群机器人 file/voice 约 20MB）。
const maxUploadBytes = 20 << 20

// RegisterUploadMedia 挂载 POST /cgi-bin/webhook/upload_media?key=xxx&type=file|voice
// （企微群机器人专用素材接口，与自建应用 media/upload 不同）。
func RegisterUploadMedia(mux *http.ServeMux, st *store.Store) {
	mux.HandleFunc("POST /cgi-bin/webhook/upload_media", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			writeErrcode(w, errcodeBadKey, "invalid webhook key")
			return
		}
		if _, _, err := st.GetChatByWebhookKey(key); err != nil {
			writeErrcode(w, errcodeBadKey, "invalid webhook key")
			return
		}
		mediaType := r.URL.Query().Get("type")
		if mediaType != "file" && mediaType != "voice" {
			writeErrcode(w, 40004, "invalid media type") // 40004: 不合法的媒体文件类型
			return
		}
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			writeErrcode(w, 40005, "invalid media file") // 40005: 不合法的文件类型/解析失败
			return
		}
		file, header, err := r.FormFile("media")
		if err != nil {
			writeErrcode(w, 40005, "media field missing")
			return
		}
		defer file.Close()

		mediaID := store.NewRandomString(24)
		media, err := st.SaveMedia(mediaID, mediaType, header.Filename, int64(mediaTTL.Seconds()))
		if err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		if err := os.MkdirAll(filepath.Dir(media.FilePath), 0o755); err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		out, err := os.Create(media.FilePath)
		if err != nil {
			writeErrcode(w, 500, "internal error")
			return
		}
		n, err := io.Copy(out, io.LimitReader(file, maxUploadBytes))
		_ = out.Close()
		if err != nil || n == 0 {
			_ = os.Remove(media.FilePath)
			writeErrcode(w, 40005, "invalid media file")
			return
		}
		writeJSON(w, map[string]any{
			"errcode":    0,
			"errmsg":     "ok",
			"type":       mediaType,
			"media_id":   mediaID,
			"created_at": time.Unix(media.CreatedAt, 0).Format("2006-01-02 15:04:05"),
		})
	})
}
