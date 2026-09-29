package relief

import (
	"testing"
	"time"
)

func passingInput() InspectionInput {
	return InspectionInput{Valve: "PSV-101", TestPressure: 1.8, OpenMS: 500, CloseMS: 900, ReseatResult: true}
}

func TestInspectionPassIssuesUsableCertificate(t *testing.T) {
	book := NewInspectionBook()
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	inspection, certificate, err := book.Record(passingInput(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Passed {
		t.Fatal("inspection should pass")
	}
	if !certificate.Valid || !certificate.Usable(now) {
		t.Fatal("certificate should be usable")
	}
	plan := book.Plan("PSV-101", now)
	if !plan.Available || plan.Blocked() {
		t.Fatal("shutdown plan should be available with valid certificate")
	}
}

func TestInspectionValidation(t *testing.T) {
	book := NewInspectionBook()
	now := time.Now()
	cases := []InspectionInput{
		{Valve: "  ", TestPressure: 1, OpenMS: 1, CloseMS: 1, ReseatResult: true},
		{Valve: "PSV-1", TestPressure: 0, OpenMS: 1, CloseMS: 1, ReseatResult: true},
		{Valve: "PSV-1", TestPressure: 1, OpenMS: 0, CloseMS: 1, ReseatResult: true},
	}
	for i, input := range cases {
		if _, _, err := book.Record(input, now); err == nil {
			t.Fatalf("case %d should be rejected", i)
		}
	}
}

func TestInspectionFailureRejectsCertificate(t *testing.T) {
	book := NewInspectionBook()
	now := time.Now()

	failed := passingInput()
	failed.ReseatResult = false
	_, certificate, err := book.Record(failed, now)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.Valid || certificate.Usable(now) {
		t.Fatal("failed inspection must not produce a usable certificate")
	}
	plan := book.Plan("PSV-101", now)
	if plan.Available || !plan.Blocked() {
		t.Fatal("failed inspection must block the shutdown plan")
	}
	if plan.Reason == "" {
		t.Fatal("plan should explain why the valve is unavailable")
	}

	slow := passingInput()
	slow.OpenMS = DefaultMaxResponseMS + 1
	inspection, _, _ := book.Record(slow, now)
	if inspection.Passed || inspection.Reason == "" {
		t.Fatal("slow opening response must fail the inspection")
	}
}

func TestExpiredCertificateUnavailable(t *testing.T) {
	book := NewInspectionBook()
	issued := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	_, certificate, err := book.Record(passingInput(), issued)
	if err != nil {
		t.Fatal(err)
	}
	if !certificate.Valid {
		t.Fatal("certificate should have been valid when issued")
	}
	now := issued.Add(DefaultCertificateTTL + time.Hour)
	if certificate.Usable(now) || !certificate.Expired(now) {
		t.Fatal("certificate should be expired")
	}
	plan := book.Plan("PSV-101", now)
	if plan.Available || !plan.Blocked() {
		t.Fatal("expired certificate must block the shutdown plan")
	}

	// 重新定检后恢复可用，且旧处置记录不再影响新证书。
	_, newCertificate, err := book.Record(passingInput(), now)
	if err != nil || !newCertificate.Usable(now) {
		t.Fatal("re-inspection should restore a usable certificate")
	}
	if !book.Plan("PSV-101", now.Add(time.Hour)).Available {
		t.Fatal("plan should be available after re-inspection")
	}
}

func TestDispositionAllowsProceedingButRemainsRecorded(t *testing.T) {
	book := NewInspectionBook()
	now := time.Now()
	if _, err := book.RecordDisposition("PSV-101", "  ", "ops", now); err == nil {
		t.Fatal("empty measure must be rejected")
	}
	// 从未定检且未附加当次处置：阻断。
	blocked := book.Plan("PSV-101", now)
	if blocked.Available || !blocked.Blocked() || blocked.ByDisposition() {
		t.Fatal("plan without current disposition must be blocked")
	}
	disposition, err := book.RecordDisposition("PSV-101", "manual isolation with flare standby watch", "ops-A", now)
	if err != nil {
		t.Fatal(err)
	}
	// 历史处置不会自动放行，必须由本次计划显式附加。
	if book.Plan("PSV-101", now).ByDisposition() {
		t.Fatal("historical disposition must not auto-authorize later plans")
	}
	plan := book.Plan("PSV-101", now).WithDisposition(disposition)
	if plan.Available || plan.Blocked() || !plan.ByDisposition() {
		t.Fatal("unavailable valve with current disposition should proceed by disposition, not be blocked")
	}
	if plan.Alternative == nil || plan.Alternative.ID != disposition.ID || plan.Alternative.Measure == "" {
		t.Fatal("plan should retain the alternative disposition record")
	}
	if len(book.Dispositions("PSV-101")) != 1 {
		t.Fatal("disposition history should be retained")
	}
}
