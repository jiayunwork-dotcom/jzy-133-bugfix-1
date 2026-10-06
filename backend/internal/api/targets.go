package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"spc/internal/domain"
)

type targetReq struct {
	Name         string     `json:"name"`
	Machine      string     `json:"machine"`
	Dimension    string     `json:"dimension"`
	USL          *optNumber `json:"usl"`
	LSL          *optNumber `json:"lsl"`
	SubgroupN    int        `json:"subgroupN"`
	EnabledRules []int      `json:"enabledRules"`
}

func (r targetReq) toSpec() domain.TargetSpec {
	spec := domain.TargetSpec{
		Name:         r.Name,
		Machine:      r.Machine,
		Dimension:    r.Dimension,
		SubgroupN:    r.SubgroupN,
		EnabledRules: r.EnabledRules,
	}
	if r.USL != nil {
		spec.USL = r.USL.ptr()
	}
	if r.LSL != nil {
		spec.LSL = r.LSL.ptr()
	}
	return spec
}

func (h *Handler) createTarget(c echo.Context) error {
	var req targetReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	t, err := h.svc.CreateTarget(c.Request().Context(), req.toSpec())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, t)
}

func (h *Handler) listTargets(c echo.Context) error {
	list, err := h.svc.ListTargets(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, list)
}

func (h *Handler) getTarget(c echo.Context) error {
	id, err := idParam(c)
	if err != nil {
		return badRequest(err.Error())
	}
	t, err := h.svc.GetTarget(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, t)
}

func badRequest(msg string) *echo.HTTPError {
	return echo.NewHTTPError(http.StatusBadRequest, msg)
}
