package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"spc/internal/domain"
	"spc/internal/service"
)

// Handler 持有业务服务，注册全部路由。
type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

// Register 在 Echo 上注册路由。
func (h *Handler) Register(e *echo.Echo) {
	e.HTTPErrorHandler = errorHandler

	e.GET("/api/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	e.GET("/api/targets", h.listTargets)
	e.POST("/api/targets", h.createTarget)
	e.GET("/api/targets/:id", h.getTarget)
	e.GET("/api/targets/:id/series", h.getSeries)
	e.POST("/api/targets/:id/ingest", h.ingest)
	e.POST("/api/targets/:id/baselines", h.createBaseline)
}

func idParam(c echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("监控对象 id 必须是正整数")
	}
	return id, nil
}

// bindJSON 解析请求体，把绑定/类型错误统一转成带清晰原因的 400。
func bindJSON(c echo.Context, dst any) error {
	if err := c.Bind(dst); err != nil {
		var he *echo.HTTPError
		if errors.As(err, &he) {
			return echo.NewHTTPError(http.StatusBadRequest,
				fmt.Sprintf("请求体格式错误：%v", he.Message))
		}
		return echo.NewHTTPError(http.StatusBadRequest, "请求体不是合法 JSON")
	}
	return nil
}

// errorHandler 把领域错误映射为合适的 HTTP 状态码与中文提示。
func errorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	status := http.StatusInternalServerError
	msg := err.Error()

	var he *echo.HTTPError
	if errors.As(err, &he) {
		status = he.Code
		if m, ok := he.Message.(string); ok {
			msg = m
		}
	}

	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrValidation),
		errors.Is(err, domain.ErrBadValues),
		errors.Is(err, domain.ErrSizeMismatch),
		errors.Is(err, domain.ErrBaselineTooShort),
		errors.Is(err, domain.ErrRangeExceedsData),
		errors.Is(err, domain.ErrNoBaseline):
		status = http.StatusBadRequest
	}

	if status >= 500 {
		c.Logger().Error(err)
		msg = "服务器内部错误"
	}
	_ = c.JSON(status, map[string]string{"error": msg})
}
