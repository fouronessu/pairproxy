package proxy

import (
	"net/http"

	"go.uber.org/zap"
)

// debugBodyMaxBytes 单条 debug 日志中 body 的最大字节数（64 KB）。
const debugBodyMaxBytes = 64 * 1024

// sanitizeHeaders 将请求/响应头转换为日志字段，自动过滤敏感 key。
// 过滤：Authorization（API Key）、X-Pairproxy-Auth（用户 JWT）、Cookie。
func sanitizeHeaders(h http.Header) zap.Field {
	safe := make(map[string]string, len(h))
	skip := map[string]bool{
		"Authorization":   true,
		"X-Pairproxy-Auth": true,
		"Cookie":          true,
	}
	for k, vs := range h {
		if skip[http.CanonicalHeaderKey(k)] {
			continue
		}
		if len(vs) > 0 {
			safe[k] = vs[0]
		}
	}
	return zap.Any("headers", safe)
}

// truncate 截断字节片到指定最大长度，防止超大 body 撑爆日志文件。
func truncate(b []byte, max int) []byte {
	if len(b) <= max {
		return b
	}
	return b[:max]
}

// debugResponseWriter 包装 http.ResponseWriter，在不影响正常转发的情况下，
// 将每个 Write 调用同步记录到 debugLogger。
// 用于 cproxy 中记录 s-proxy 返回的 streaming 响应内容。
type debugResponseWriter struct {
	http.ResponseWriter
	logger *zap.Logger
	reqID  string
}

func (d *debugResponseWriter) Write(p []byte) (int, error) {
	n, err := d.ResponseWriter.Write(p)
	if n > 0 {
		d.logger.Debug("← sproxy response chunk",
			zap.String("request_id", d.reqID),
			zap.ByteString("data", truncate(p[:n], debugBodyMaxBytes)),
		)
	}
	return n, err
}

func (d *debugResponseWriter) Flush() {
	if f, ok := d.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
