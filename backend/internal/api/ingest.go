package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"spc/internal/service"
)

type ingestReq struct {
	Values  []number `json:"values"`
	Grouped bool     `json:"grouped"`
}

func (h *Handler) ingest(c echo.Context) error {
	id, err := idParam(c)
	if err != nil {
		return badRequest(err.Error())
	}
	var req ingestReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	vals := make([]float64, len(req.Values))
	for i, v := range req.Values {
		vals[i] = float64(v)
	}

	res, err := h.svc.Ingest(c.Request().Context(), service.IngestRequest{
		TargetID: id,
		Values:   vals,
		Grouped:  req.Grouped,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

type baselineReq struct {
	StartSeq int `json:"startSeq"`
	EndSeq   int `json:"endSeq"`
}

func (h *Handler) createBaseline(c echo.Context) error {
	id, err := idParam(c)
	if err != nil {
		return badRequest(err.Error())
	}
	var req baselineReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.StartSeq == 0 {
		req.StartSeq = 1
	}
	bl, err := h.svc.CreateBaseline(c.Request().Context(), id, req.StartSeq, req.EndSeq)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, bl)
}

func (h *Handler) getSeries(c echo.Context) error {
	id, err := idParam(c)
	if err != nil {
		return badRequest(err.Error())
	}
	dto, err := h.svc.GetSeries(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}
