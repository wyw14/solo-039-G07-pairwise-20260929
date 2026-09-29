package relief

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultMaxResponseMS 是启闭响应时间的默认合格上限（毫秒）。
	DefaultMaxResponseMS int64 = 2000
	// DefaultCertificateTTL 是投用证书默认有效期：安全阀每年至少定检一次。
	DefaultCertificateTTL time.Duration = 365 * 24 * time.Hour
)

// Inspection 录入一次投用前定检：阀门编号、测试压力、启闭响应与回座结果。
type Inspection struct {
	ID             uuid.UUID
	Valve          string
	TestPressure   float64
	OpenMS         int64 // 开启响应耗时（毫秒）
	CloseMS        int64 // 关闭（回座）响应耗时（毫秒）
	ReseatResult   bool  // 回座结果：回座密封是否合格
	ReseatPressure float64
	MaxResponseMS  int64
	Passed         bool
	Reason         string
	At             time.Time
}

// InspectionInput 是定检录入参数；零值字段采用默认判定门槛与有效期。
type InspectionInput struct {
	Valve          string
	TestPressure   float64
	OpenMS         int64
	CloseMS        int64
	ReseatResult   bool
	ReseatPressure float64
	MaxResponseMS  int64
	Validity       time.Duration
}

// Certificate 是定检合格后签发的投用证书，带有效期。
type Certificate struct {
	ID           uuid.UUID
	Valve        string
	InspectionID uuid.UUID
	TestPressure float64
	IssuedAt     time.Time
	ExpiresAt    time.Time
	Valid        bool
	Reason       string
}

// Usable 表示证书在 at 时刻可作为投用依据。
func (c Certificate) Usable(at time.Time) bool {
	return c.Valid && !at.Before(c.IssuedAt) && at.Before(c.ExpiresAt)
}

// Expired 表示证书本身合格但已过有效期。
func (c Certificate) Expired(at time.Time) bool { return c.Valid && !at.Before(c.ExpiresAt) }

// Disposition 记录证书不可用时采取的替代处置措施。
type Disposition struct {
	ID       uuid.UUID
	Valve    string
	Measure  string
	Operator string
	At       time.Time
}

// Plan 是安全停机计划对定检依据的可用性评估。
type Plan struct {
	Valve          string
	Available      bool
	HasCertificate bool
	Certificate    Certificate
	Reason         string
	Alternative    *Disposition
}

// Blocked 表示阀门不可用且没有已登记的替代处置，停机计划必须拒绝。
func (p Plan) Blocked() bool { return !p.Available && p.Alternative == nil }

// ByDisposition 表示阀门本身不可用，但凭已登记的替代处置措施放行。
func (p Plan) ByDisposition() bool { return !p.Available && p.Alternative != nil }

// WithDisposition 在证书不可用时为本次停机计划附加当次替代处置。
// 历史处置记录仅作留存，不会自动成为后续计划的放行依据。
func (p Plan) WithDisposition(disposition Disposition) Plan {
	p.Alternative = &disposition
	return p
}

// InspectionBook 是定检记录、投用证书与替代处置的台账。
type InspectionBook struct {
	mu           sync.Mutex
	inspections  map[string][]Inspection
	certificates map[string]Certificate
	dispositions map[string][]Disposition
}

func NewInspectionBook() *InspectionBook {
	return &InspectionBook{
		inspections:  make(map[string][]Inspection),
		certificates: make(map[string]Certificate),
		dispositions: make(map[string][]Disposition),
	}
}

// Record 录入一次定检并据此签发（或拒发）投用证书。重新定检会以新证书覆盖旧证书。
func (b *InspectionBook) Record(input InspectionInput, at time.Time) (Inspection, Certificate, error) {
	valve := strings.TrimSpace(input.Valve)
	if valve == "" {
		return Inspection{}, Certificate{}, errors.New("relief valve identifier is required")
	}
	if len(valve) > 32 {
		return Inspection{}, Certificate{}, errors.New("relief valve identifier is too long")
	}
	if input.TestPressure <= 0 {
		return Inspection{}, Certificate{}, errors.New("test pressure must be positive")
	}
	if input.OpenMS <= 0 || input.CloseMS <= 0 {
		return Inspection{}, Certificate{}, errors.New("open and close response times are required")
	}
	maxResponse := input.MaxResponseMS
	if maxResponse <= 0 {
		maxResponse = DefaultMaxResponseMS
	}
	inspection := Inspection{
		ID:             uuid.New(),
		Valve:          valve,
		TestPressure:   input.TestPressure,
		OpenMS:         input.OpenMS,
		CloseMS:        input.CloseMS,
		ReseatResult:   input.ReseatResult,
		ReseatPressure: input.ReseatPressure,
		MaxResponseMS:  maxResponse,
		At:             at,
	}
	switch {
	case input.OpenMS > maxResponse:
		inspection.Reason = "opening response exceeded limit"
	case input.CloseMS > maxResponse:
		inspection.Reason = "closing response exceeded limit"
	case !input.ReseatResult:
		inspection.Reason = "reseat result unsatisfactory"
	default:
		inspection.Passed = true
	}
	validity := input.Validity
	if validity <= 0 {
		validity = DefaultCertificateTTL
	}
	certificate := Certificate{
		ID:           uuid.New(),
		Valve:        valve,
		InspectionID: inspection.ID,
		TestPressure: input.TestPressure,
		IssuedAt:     at,
		ExpiresAt:    at.Add(validity),
		Valid:        inspection.Passed,
		Reason:       inspection.Reason,
	}
	b.mu.Lock()
	b.inspections[valve] = append(b.inspections[valve], inspection)
	b.certificates[valve] = certificate
	b.mu.Unlock()
	return inspection, certificate, nil
}

// RecordDisposition 保留一条替代处置记录（如手动隔离、备用火炬旁站）。
func (b *InspectionBook) RecordDisposition(valve, measure, operator string, at time.Time) (Disposition, error) {
	valve = strings.TrimSpace(valve)
	if valve == "" {
		return Disposition{}, errors.New("relief valve identifier is required")
	}
	if strings.TrimSpace(measure) == "" {
		return Disposition{}, errors.New("alternative disposition measure is required")
	}
	disposition := Disposition{
		ID:       uuid.New(),
		Valve:    valve,
		Measure:  strings.TrimSpace(measure),
		Operator: strings.TrimSpace(operator),
		At:       at,
	}
	b.mu.Lock()
	b.dispositions[valve] = append(b.dispositions[valve], disposition)
	b.mu.Unlock()
	return disposition, nil
}

// Certificate 返回阀门当前生效中的最新证书（可能已过期或不合格）。
func (b *InspectionBook) Certificate(valve string) (Certificate, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	certificate, ok := b.certificates[valve]
	return certificate, ok
}

// Latest 返回阀门最近一次定检记录。
func (b *InspectionBook) Latest(valve string) (Inspection, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries := b.inspections[valve]
	if len(entries) == 0 {
		return Inspection{}, false
	}
	return entries[len(entries)-1], true
}

// Disposition 返回阀门最近一条替代处置记录。
func (b *InspectionBook) Disposition(valve string) (Disposition, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries := b.dispositions[valve]
	if len(entries) == 0 {
		return Disposition{}, false
	}
	return entries[len(entries)-1], true
}

func (b *InspectionBook) History(valve string) []Inspection {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Inspection(nil), b.inspections[valve]...)
}

func (b *InspectionBook) Dispositions(valve string) []Disposition {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Disposition(nil), b.dispositions[valve]...)
}

// Plan 评估 at 时刻安全停机计划能否以该阀门的定检证书为依据。
// 返回的计划不自动携带历史替代处置；需要凭替代处置放行时由调用方显式附加。
func (b *InspectionBook) Plan(valve string, at time.Time) Plan {
	b.mu.Lock()
	certificate, hasCertificate := b.certificates[valve]
	b.mu.Unlock()

	plan := Plan{Valve: valve, HasCertificate: hasCertificate, Certificate: certificate}
	switch {
	case !hasCertificate:
		plan.Reason = "no inspection certificate before commissioning"
	case !certificate.Valid:
		plan.Reason = "periodic inspection failed: " + certificate.Reason
	case certificate.Expired(at):
		plan.Reason = "inspection certificate expired"
	default:
		plan.Available = true
	}
	return plan
}
