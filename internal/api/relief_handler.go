package api

import (
	"github.com/wyw14/solo-039/internal/relief"
	"net/http"
	"time"
)

func (s *Server) reliefInspection(w http.ResponseWriter, r *http.Request) {
	var input reliefInspectionRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	testedAt := time.Now()
	if input.TestedAt != 0 {
		testedAt = time.Unix(input.TestedAt, 0)
	}
	in := relief.Inspection{
		Valve:        input.Valve,
		SetPressure:  input.SetPressure,
		TestPressure: input.TestPressure,
		Response:     relief.ResponseResult(input.Response),
		Reseat:       relief.ReseatResult(input.Reseat),
		TestedAt:     testedAt,
	}
	recorded, result, cert, err := s.plant.RecordReliefInspection(in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	status := http.StatusAccepted
	body := map[string]any{"inspection": recorded, "passed": result.Passed(), "message": result.Message}
	if result.Passed() {
		body["certificate"] = cert
	} else {
		// 定检失败属于正常业务结论而非请求错误，但不应返回 2xx 成功签发语义。
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, body)
}

func (s *Server) reliefDisposition(w http.ResponseWriter, r *http.Request) {
	var input reliefDispositionRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now()
	d := relief.Disposition{
		Valve:     input.Valve,
		Reason:    input.Reason,
		Action:    input.Action,
		Operator:  input.Operator,
		At:        now,
		ValidFrom: now,
	}
	if input.ValidForHours > 0 {
		d.ValidUntil = now.Add(time.Duration(input.ValidForHours) * time.Hour)
	}
	recorded, err := s.plant.RecordReliefDisposition(d)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, recorded)
}

func (s *Server) reliefCertificate(w http.ResponseWriter, r *http.Request) {
	valve := r.URL.Query().Get("valve_id")
	if valve == "" {
		valve = "flare"
	}
	writeJSON(w, http.StatusOK, s.plant.ReliefCertificate(valve))
}
