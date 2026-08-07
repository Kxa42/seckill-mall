// Package httpx 提供版本化 HTTP API 的统一响应和错误格式。
package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Envelope 是所有新 HTTP API 的统一响应结构。
type Envelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// OK 返回成功响应。
func OK(c *gin.Context, status int, data any) {
	c.JSON(status, Envelope{
		Code:      "OK",
		Message:   "成功",
		Data:      data,
		RequestID: RequestID(c),
	})
}

// Error 返回不包含内部错误细节的失败响应。
func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, Envelope{
		Code:      code,
		Message:   message,
		RequestID: RequestID(c),
	})
}

// RequestID 获取或生成当前请求标识。
func RequestID(c *gin.Context) string {
	if value, exists := c.Get("request_id"); exists {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return c.GetHeader("X-Request-ID")
}

// NoContent 返回无响应体成功状态。
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
