package api

import (
	"net/http"
	"time"

	"github.com/wyw14/solo-039/internal/relief"
)

// recordInspection 录入泄压阀投用前定检，合格则签发带有效期的投用证书。
func (s *Server) recordInspection(w http.ResponseWriter, r *http.Request) {
	var input inspectionRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	validity := time.Duration(0)
	if input.ValidityHours > 0 {
		validity = time.Duration(input.ValidityHours) * time.Hour
	}
	inspection, certificate, err := s.plant.RecordReliefInspection(relief.InspectionInput{
		Valve:          input.Valve,
		TestPressure:   input.TestPressure,
		OpenMS:         input.OpenMS,
		CloseMS:        input.CloseMS,
		ReseatResult:   input.ReseatResult,
		ReseatPressure: input.ReseatPressure,
		MaxResponseMS:  input.MaxResponseMS,
		Validity:       validity,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	status := http.StatusAccepted
	if !certificate.Valid {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, map[string]any{"inspection": inspection, "certificate": certificate})
}

// reliefCertificate 查询阀门投用证书及安全停机计划的可用性评估。
func (s *Server) reliefCertificate(w http.ResponseWriter, r *http.Request) {
	valve := r.URL.Query().Get("valve")
	if valve == "" {
		valve = "flare"
	}
	book := s.plant.ReliefInspections
	plan := book.Plan(valve, time.Now())
	response := map[string]any{"plan": plan}
	if certificate, ok := book.Certificate(valve); ok {
		response["certificate"] = certificate
	}
	if inspection, ok := book.Latest(valve); ok {
		response["latest_inspection"] = inspection
	}
	response["dispositions"] = book.Dispositions(valve)
	writeJSON(w, http.StatusOK, response)
}

// recordDisposition 登记证书不可用（定检失败/过期/未定检）时的替代处置。
func (s *Server) recordDisposition(w http.ResponseWriter, r *http.Request) {
	var input dispositionRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	disposition, err := s.plant.RecordReliefDisposition(input.Valve, input.Measure, input.Operator)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, disposition)
}
