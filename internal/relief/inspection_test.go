package relief

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func passingInspection(valve string, at time.Time) Inspection {
	return Inspection{
		Valve:        valve,
		SetPressure:  1.00,
		TestPressure: 1.02,
		Response:     ResponsePass,
		Reseat:       ReseatPass,
		TestedAt:     at,
	}
}

func TestRecordInspectionPassingIssuesCertificate(t *testing.T) {
	book := NewBook()
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)

	in, result, cert, err := book.RecordInspection(passingInspection("PSV-101", now), now)
	if err != nil {
		t.Fatalf("record inspection: %v", err)
	}
	if !result.Passed() {
		t.Fatalf("expected pass, got %s", result.Message)
	}
	if cert.ID == uuid.Nil {
		t.Fatal("certificate not issued")
	}
	if cert.Valve != "PSV-101" || cert.Inspection.ID != in.ID {
		t.Fatal("certificate not linked to inspection")
	}
	if !cert.ValidAt(now) {
		t.Fatal("certificate should be valid at issue time")
	}
	if !cert.ValidAt(now.Add(DefaultValidity - time.Hour)) {
		t.Fatal("certificate should be valid before expiry")
	}
	if cert.ValidAt(now.Add(DefaultValidity + time.Hour)) {
		t.Fatal("certificate should be invalid after expiry")
	}

	availability := book.CheckValve("PSV-101", now)
	if !availability.Available || availability.Reason != "" {
		t.Fatalf("valve should be available: %+v", availability)
	}
}

func TestRecordInspectionFailuresIssueNoCertificate(t *testing.T) {
	cases := map[string]Inspection{
		"slow response":             {Valve: "PSV-201", SetPressure: 1.0, TestPressure: 1.0, Response: ResponseSlow, Reseat: ReseatPass, TestedAt: time.Now()},
		"failed response":           {Valve: "PSV-202", SetPressure: 1.0, TestPressure: 1.0, Response: ResponseFail, Reseat: ReseatPass, TestedAt: time.Now()},
		"reseat leak":               {Valve: "PSV-203", SetPressure: 1.0, TestPressure: 1.0, Response: ResponsePass, Reseat: ReseatLeak, TestedAt: time.Now()},
		"pressure out of tolerance": {Valve: "PSV-204", SetPressure: 1.0, TestPressure: 1.20, Response: ResponsePass, Reseat: ReseatPass, TestedAt: time.Now()},
		"reseat skipped":            {Valve: "PSV-205", SetPressure: 1.0, TestPressure: 1.0, Response: ResponsePass, Reseat: ReseatNotTested, TestedAt: time.Now()},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			book := NewBook()
			_, result, cert, err := book.RecordInspection(in, time.Now())
			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
			if result.Passed() {
				t.Fatal("inspection should fail")
			}
			if cert.ID != uuid.Nil || cert.Valve != "" {
				t.Fatal("failed inspection must not issue a certificate")
			}
			availability := book.CheckValve(in.Valve, time.Now())
			if availability.Available || availability.Reason != ReasonInspectionFailed {
				t.Fatalf("expected failed_inspection, got %+v", availability)
			}
		})
	}
}

func TestInspectionValidation(t *testing.T) {
	now := time.Now()
	cases := map[string]Inspection{
		"missing valve":     {SetPressure: 1, TestPressure: 1, Response: ResponsePass, Reseat: ReseatPass, TestedAt: now},
		"missing pressures": {Valve: "PSV-301", Response: ResponsePass, Reseat: ReseatPass, TestedAt: now},
		"bad response":      {Valve: "PSV-301", SetPressure: 1, TestPressure: 1, Response: "bogus", Reseat: ReseatPass, TestedAt: now},
		"bad reseat":        {Valve: "PSV-301", SetPressure: 1, TestPressure: 1, Response: ResponsePass, Reseat: "bogus", TestedAt: now},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := NewBook().RecordInspection(in, now); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	t.Run("missing test time via Validate", func(t *testing.T) {
		in := Inspection{Valve: "PSV-301", SetPressure: 1, TestPressure: 1, Response: ResponsePass, Reseat: ReseatPass}
		if err := in.Validate(); err == nil {
			t.Fatal("expected validation error for zero test time")
		}
	})
}

func TestCertificateExpiredMakesValveUnavailable(t *testing.T) {
	book := NewBook()
	issued := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, _, _, err := book.RecordInspection(passingInspection("PSV-401", issued), issued); err != nil {
		t.Fatal(err)
	}
	later := issued.Add(DefaultValidity + 30*24*time.Hour)
	availability := book.CheckValve("PSV-401", later)
	if availability.Available || availability.Reason != ReasonCertificateExpired {
		t.Fatalf("expected certificate_expired, got %+v", availability)
	}
	if !availability.CertPresent {
		t.Fatal("expired certificate should still be returned for reference")
	}
}

func TestFailedReinspectionSupersedesValidCertificate(t *testing.T) {
	book := NewBook()
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, _, _, err := book.RecordInspection(passingInspection("PSV-501", issued), issued); err != nil {
		t.Fatal(err)
	}
	failed := passingInspection("PSV-501", issued.AddDate(0, 6, 0))
	failed.Response = ResponseSlow
	if _, result, cert, err := book.RecordInspection(failed, issued.AddDate(0, 6, 0)); err != nil {
		t.Fatal(err)
	} else if result.Passed() || cert.Valve != "" {
		t.Fatal("re-inspection must fail and must not issue a certificate")
	}
	availability := book.CheckValve("PSV-501", issued.AddDate(0, 6, 1))
	if availability.Available || availability.Reason != ReasonInspectionFailed {
		t.Fatalf("latest failed inspection must supersede old certificate: %+v", availability)
	}
}

func TestUnknownValveMissingCertificate(t *testing.T) {
	availability := NewBook().CheckValve("PSV-999", time.Now())
	if availability.Available || availability.Reason != ReasonCertificateMissing {
		t.Fatalf("expected certificate_missing, got %+v", availability)
	}
}

func TestDispositionCoversUnavailableValve(t *testing.T) {
	book := NewBook()
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)

	in := passingInspection("PSV-601", now.AddDate(-2, 0, 0))
	if _, _, _, err := book.RecordInspection(in, now.AddDate(-2, 0, 0)); err != nil {
		t.Fatal(err)
	}
	availability := book.CheckValve("PSV-601", now)
	if availability.Available || availability.Reason != ReasonCertificateExpired {
		t.Fatalf("precondition: expected expired cert, got %+v", availability)
	}

	d, err := book.RecordDisposition(Disposition{
		Valve:      "PSV-601",
		Reason:     string(ReasonCertificateExpired),
		Action:     "isolate with spectacle blind and operate at reduced pressure under operator watch",
		Operator:   "op-chen",
		At:         now,
		ValidFrom:  now,
		ValidUntil: now.Add(72 * time.Hour),
	})
	if err != nil {
		t.Fatalf("record disposition: %v", err)
	}

	availability = book.CheckValve("PSV-601", now.Add(24*time.Hour))
	if availability.Available {
		t.Fatal("an active disposition does not recertify the valve")
	}
	if !availability.DispoPresent || availability.Disposition.ID != d.ID {
		t.Fatalf("expected active disposition to be retained, got %+v", availability)
	}

	// 替代处置到期后不再兜底。
	expired := book.CheckValve("PSV-601", now.Add(73*time.Hour))
	if expired.Available || expired.DispoPresent {
		t.Fatalf("expired disposition must not cover the valve: %+v", expired)
	}
}

func TestDispositionValidation(t *testing.T) {
	now := time.Now()
	if _, err := NewBook().RecordDisposition(Disposition{Action: "x", At: now}); err == nil {
		t.Fatal("expected valve validation error")
	}
	if _, err := NewBook().RecordDisposition(Disposition{Valve: "PSV-701", At: now}); err == nil {
		t.Fatal("expected action validation error")
	}
	if _, err := NewBook().RecordDisposition(Disposition{Valve: "PSV-701", Action: "x"}); err == nil {
		t.Fatal("expected time validation error")
	}
}

func TestPlanShutdownReflectsBookState(t *testing.T) {
	coordinator := NewCoordinator()
	now := time.Now()
	if coordinator.PlanShutdown("flare", now).Available {
		t.Fatal("valve without inspection certificate cannot be commissioned")
	}
	if _, _, _, err := coordinator.Book.RecordInspection(passingInspection("flare", now), now); err != nil {
		t.Fatal(err)
	}
	plan := coordinator.PlanShutdown("flare", now)
	if !plan.Available {
		t.Fatal("certified valve should be cleared for shutdown")
	}
	result := CombineShutdown(nil, nil, coordinator.State.Snapshot(), true, plan)
	if !result.Certified() || result.CoveredByDisposition() {
		t.Fatalf("unexpected result flags: %+v", result)
	}
}
