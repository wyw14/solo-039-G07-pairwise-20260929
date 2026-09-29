package service

import (
	"context"
	"github.com/wyw14/solo-039/internal/relief"
	"strings"
	"testing"
	"time"
)

func passingFlareInspection() relief.Inspection {
	return relief.Inspection{
		Valve:        "flare",
		SetPressure:  1.00,
		TestPressure: 1.01,
		Response:     relief.ResponsePass,
		Reseat:       relief.ReseatPass,
		TestedAt:     time.Now(),
	}
}

func TestShutdownBlockedWithoutCertificate(t *testing.T) {
	plant := NewPlant(t.TempDir())

	result, err := plant.Shutdown(context.Background(), false)
	if err == nil {
		t.Fatal("shutdown must be rejected without a valid relief valve certificate")
	}
	if !strings.Contains(err.Error(), string(relief.ReasonCertificateMissing)) {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ValveAvailable || result.CoveredByDisposition() {
		t.Fatalf("shutdown result should show unavailable valve: %+v", result)
	}
	// 被闸门拦下时不得执行泄压动作。
	if plant.Snapshot().Relief.Released || plant.Snapshot().Relief.TimedOut {
		t.Fatal("relief action must not run while the valve is unavailable")
	}
	if plant.Incidents.CountOpen() == 0 {
		t.Fatal("blocked shutdown must open an incident")
	}
}

func TestShutdownAllowedWithValidCertificate(t *testing.T) {
	plant := NewPlant(t.TempDir())
	if _, result, _, err := plant.RecordReliefInspection(passingFlareInspection()); err != nil || !result.Passed() {
		t.Fatalf("inspection not accepted: result=%+v err=%v", result, err)
	}

	shutdownResult, err := plant.Shutdown(context.Background(), false)
	if err != nil {
		t.Fatalf("certified valve should permit shutdown: %v", err)
	}
	if !shutdownResult.Certified() || shutdownResult.CoveredByDisposition() {
		t.Fatalf("shutdown should run under certificate, not disposition: %+v", shutdownResult)
	}
}

func TestShutdownUnderActiveDispositionKeepsRecord(t *testing.T) {
	plant := NewPlant(t.TempDir())

	// 定检失败：启闭迟缓，不签发证书。
	failed := passingFlareInspection()
	failed.Response = relief.ResponseSlow
	if _, result, cert, err := plant.RecordReliefInspection(failed); err != nil {
		t.Fatalf("record failed inspection: %v", err)
	} else if result.Passed() || cert.Valve != "" {
		t.Fatal("failed inspection must not issue a certificate")
	}

	// 无替代处置时仍被拒绝。
	if _, err := plant.Shutdown(context.Background(), false); err == nil {
		t.Fatal("shutdown must stay blocked without an alternative disposition")
	}

	now := time.Now()
	if _, err := plant.RecordReliefDisposition(relief.Disposition{
		Valve:      "flare",
		Reason:     string(relief.ReasonInspectionFailed),
		Action:     "standby PSV-101B commissioned; operate at reduced pressure",
		Operator:   "tester",
		At:         now,
		ValidFrom:  now,
		ValidUntil: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("record disposition: %v", err)
	}

	result, err := plant.Shutdown(context.Background(), false)
	if err != nil {
		t.Fatalf("active disposition should permit degraded shutdown: %v", err)
	}
	if result.ValveAvailable {
		t.Fatal("a disposition must not recertify the valve")
	}
	if !result.CoveredByDisposition() || result.Disposition.Action == "" {
		t.Fatalf("shutdown result must retain the alternative disposition: %+v", result)
	}
	if _, ok := plant.Journal.Last("relief.shutdown_under_disposition"); !ok {
		t.Fatal("degraded shutdown must be journaled for traceability")
	}
}

func TestExpiredDispositionDoesNotBypassGate(t *testing.T) {
	plant := NewPlant(t.TempDir())
	past := time.Now().Add(-48 * time.Hour)
	if _, err := plant.RecordReliefDisposition(relief.Disposition{
		Valve:      "flare",
		Reason:     string(relief.ReasonCertificateExpired),
		Action:     "expired blind isolation",
		Operator:   "tester",
		At:         past,
		ValidFrom:  past,
		ValidUntil: past.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Shutdown(context.Background(), false); err == nil {
		t.Fatal("an expired disposition must not bypass the certificate gate")
	}
}
