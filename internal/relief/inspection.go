package relief

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"sync"
	"time"
)

// DefaultValidity 是定检合格后投用证书的默认有效期。
var DefaultValidity = 365 * 24 * time.Hour

// ResponseResult 描述启闭响应测试结论。
type ResponseResult string

const (
	ResponsePass ResponseResult = "pass" // 在规定时间内完成启闭
	ResponseSlow ResponseResult = "slow" // 启闭迟缓/超时但最终动作
	ResponseFail ResponseResult = "fail" // 拒动
)

// ReseatResult 描述回座结果。
type ReseatResult string

const (
	ReseatPass      ReseatResult = "pass"      // 按设定压力回座且密封
	ReseatHigh      ReseatResult = "high"      // 回座压力偏高
	ReseatLeak      ReseatResult = "leak"      // 回座后泄漏
	ReseatNoReseat  ReseatResult = "noreseat"  // 不回座
	ReseatNotTested ReseatResult = "nottested" // 未做回座试验
)

// Inspection 是一次泄压阀投用前定期检验的录入记录。
type Inspection struct {
	ID           uuid.UUID
	Valve        string  // 阀门编号
	SetPressure  float64 // 铭牌整定压力(MPa)，用于核对测试压力
	TestPressure float64 // 测试/起跳压力(MPa)
	Response     ResponseResult
	Reseat       ReseatResult
	TestedAt     time.Time
}

// Spec 是判定一次定检是否合格的依据。
type Spec struct {
	// Tolerance 为测试压力相对整定压力允许的偏差(±)。
	Tolerance float64
	// 启闭响应只接受 pass；回座只接受 pass。
}

// DefaultSpec 返回常用判定依据：测试压力与整定压力偏差不超过 ±5%。
func DefaultSpec() Spec { return Spec{Tolerance: 0.05} }

// InspectionResult 是一次定检按 Spec 判定的结论。
type InspectionResult struct {
	PressureInTolerance bool
	ResponseOK          bool
	ReseatOK            bool
	Message             string
}

// Passed 报告定检是否合格。
func (r InspectionResult) Passed() bool {
	return r.PressureInTolerance && r.ResponseOK && r.ReseatOK
}

// Validate 校验录入内容本身是否完整、自洽，但不判定合格与否。
func (in Inspection) Validate() error {
	if strings.TrimSpace(in.Valve) == "" {
		return errors.New("inspection missing valve id")
	}
	if in.SetPressure <= 0 {
		return errors.New("inspection missing set pressure")
	}
	if in.TestPressure <= 0 {
		return errors.New("inspection missing test pressure")
	}
	switch in.Response {
	case ResponsePass, ResponseSlow, ResponseFail:
	default:
		return fmt.Errorf("inspection response result invalid: %q", in.Response)
	}
	switch in.Reseat {
	case ReseatPass, ReseatHigh, ReseatLeak, ReseatNoReseat, ReseatNotTested:
	default:
		return fmt.Errorf("inspection reseat result invalid: %q", in.Reseat)
	}
	if in.TestedAt.IsZero() {
		return errors.New("inspection missing test time")
	}
	return nil
}

// Evaluate 按定检依据给出结论；不修改录入记录。
func (in Inspection) Evaluate(spec Spec) InspectionResult {
	value := InspectionResult{
		PressureInTolerance: true,
		ResponseOK:          in.Response == ResponsePass,
		ReseatOK:            in.Reseat == ReseatPass,
	}
	deviation := (in.TestPressure - in.SetPressure) / in.SetPressure
	switch {
	case deviation < -spec.Tolerance || deviation > spec.Tolerance:
		value.PressureInTolerance = false
		value.Message = fmt.Sprintf("test pressure %.3f MPa out of ±%.0f%% tolerance of set pressure %.3f MPa", in.TestPressure, spec.Tolerance*100, in.SetPressure)
	case !value.ResponseOK:
		value.Message = fmt.Sprintf("open/close response %s", in.Response)
	case !value.ReseatOK:
		value.Message = fmt.Sprintf("reseat result %s", in.Reseat)
	default:
		value.Message = "periodic inspection passed"
	}
	return value
}

// Passed 报告定检是否合格。
func (in Inspection) Passed(spec Spec) bool { return in.Evaluate(spec).Passed() }

// Certificate 是定检合格后签发的、带有效期的投用证书。
type Certificate struct {
	ID         uuid.UUID
	Valve      string
	Inspection Inspection
	IssuedAt   time.Time
	ExpiresAt  time.Time
}

// ValidAt 判断证书在给定时刻是否在有效期内。
func (c Certificate) ValidAt(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.IssuedAt) && !now.After(c.ExpiresAt)
}

// ExpiredAt 判断证书是否已过期。
func (c Certificate) ExpiredAt(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}

// Disposition 是泄压阀不可投用时保留的替代处置记录(如临时盲板隔离、
// 并联备用阀投用、降压运行监护等)。
type Disposition struct {
	ID         uuid.UUID
	Valve      string
	Reason     string // failed_inspection | certificate_expired | certificate_missing
	Action     string // 采取的替代处置措施
	Operator   string // 记录人
	At         time.Time
	ValidFrom  time.Time
	ValidUntil time.Time // 替代处置本身的有效期限
}

// ActiveAt 判断替代处置在给定时刻是否仍在生效。
func (d Disposition) ActiveAt(now time.Time) bool {
	if d.ValidUntil.IsZero() {
		return !now.Before(d.ValidFrom)
	}
	return !now.Before(d.ValidFrom) && !now.After(d.ValidUntil)
}

// UnavailableReason 归纳阀门当前不可投用的原因。
type UnavailableReason string

const (
	ReasonCertificateMissing UnavailableReason = "certificate_missing"
	ReasonInspectionFailed   UnavailableReason = "failed_inspection"
	ReasonCertificateExpired UnavailableReason = "certificate_expired"
)

// Book 保存各阀门的定检记录、投用证书与替代处置记录。
type Book struct {
	mu sync.Mutex

	spec         Spec
	inspections  []Inspection
	certificates map[string]Certificate // 按阀门编号保存最新证书
	dispositions []Disposition
}

// NewBook 创建定检记录册，使用默认判定依据。
func NewBook() *Book {
	return &Book{spec: DefaultSpec(), certificates: make(map[string]Certificate)}
}

// SetSpec 替换判定依据，供测试或不同规程使用。
func (b *Book) SetSpec(spec Spec) {
	b.mu.Lock()
	b.spec = spec
	b.mu.Unlock()
}

// RecordInspection 录入一次定检：校验内容，按依据判定；合格则签发带有效期的
// 投用证书，失败则保留失败记录且不签发证书。返回记录、判定与可能签发的证书。
func (b *Book) RecordInspection(in Inspection, now time.Time) (Inspection, InspectionResult, Certificate, error) {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if in.TestedAt.IsZero() {
		in.TestedAt = now
	}
	if err := in.Validate(); err != nil {
		return Inspection{}, InspectionResult{}, Certificate{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	result := in.Evaluate(b.spec)
	b.inspections = append(b.inspections, in)
	if !result.Passed() {
		return in, result, Certificate{}, nil
	}
	cert := Certificate{ID: uuid.New(), Valve: in.Valve, Inspection: in, IssuedAt: now, ExpiresAt: now.Add(DefaultValidity)}
	b.certificates[in.Valve] = cert
	return in, result, cert, nil
}

// CertificateFor 返回某阀门当前持有的最新证书(可能已过期)。
func (b *Book) CertificateFor(valve string) (Certificate, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cert, ok := b.certificates[valve]
	return cert, ok
}

// InspectionsFor 返回某阀门的全部定检记录(最新在最后)。
func (b *Book) InspectionsFor(valve string) []Inspection {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Inspection, 0)
	for _, in := range b.inspections {
		if in.Valve == valve {
			out = append(out, in)
		}
	}
	return out
}

// LatestInspection 返回某阀门最近一次定检记录。
func (b *Book) LatestInspection(valve string) (Inspection, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.inspections) - 1; i >= 0; i-- {
		if b.inspections[i].Valve == valve {
			return b.inspections[i], true
		}
	}
	return Inspection{}, false
}

// RecordDisposition 保留一条替代处置记录。
func (b *Book) RecordDisposition(d Disposition) (Disposition, error) {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if strings.TrimSpace(d.Valve) == "" {
		return Disposition{}, errors.New("disposition missing valve id")
	}
	if strings.TrimSpace(d.Action) == "" {
		return Disposition{}, errors.New("disposition missing action")
	}
	if d.At.IsZero() {
		return Disposition{}, errors.New("disposition missing time")
	}
	if d.ValidFrom.IsZero() {
		d.ValidFrom = d.At
	}
	b.mu.Lock()
	b.dispositions = append(b.dispositions, d)
	b.mu.Unlock()
	return d, nil
}

// DispositionsFor 返回某阀门的替代处置记录。
func (b *Book) DispositionsFor(valve string) []Disposition {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Disposition, 0)
	for _, d := range b.dispositions {
		if d.Valve == valve {
			out = append(out, d)
		}
	}
	return out
}

// ActiveDispositionFor 返回某阀门在给定时刻仍有效的最新替代处置记录。
func (b *Book) ActiveDispositionFor(valve string, now time.Time) (Disposition, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.dispositions) - 1; i >= 0; i-- {
		d := b.dispositions[i]
		if d.Valve == valve && d.ActiveAt(now) {
			return d, true
		}
	}
	return Disposition{}, false
}

// Availability 是阀门投用前定检依据的核查结论。
type Availability struct {
	Valve         string
	Available     bool
	Reason        UnavailableReason
	Certificate   Certificate
	CertPresent   bool
	Latest        Inspection
	LatestPresent bool
	// 不可投用时若存在有效替代处置，则在此返回。
	Disposition  Disposition
	DispoPresent bool
	EvaluatedAt  time.Time
}

// CheckValve 按"先定检后投用"的要求核查阀门：
// 最近一次定检失败 → 不可投用(即使旧证书未到期，新检验失败即停用)；
// 从无定检/证书 → 不可投用(无证)；证书过期 → 不可投用(过期)；其余可投用。
func (b *Book) CheckValve(valve string, now time.Time) Availability {
	b.mu.Lock()
	spec := b.spec
	cert, certOK := b.certificates[valve]
	var latest Inspection
	hasInspection := false
	for i := len(b.inspections) - 1; i >= 0; i-- {
		if b.inspections[i].Valve == valve {
			latest = b.inspections[i]
			hasInspection = true
			break
		}
	}
	disposition, dispoOK := b.latestActiveDispositionLocked(valve, now)
	b.mu.Unlock()

	out := Availability{Valve: valve, EvaluatedAt: now}
	if hasInspection {
		out.Latest = latest
		out.LatestPresent = true
	}
	if certOK {
		out.Certificate = cert
		out.CertPresent = true
	}
	switch {
	case hasInspection && !latest.Passed(spec):
		out.Reason = ReasonInspectionFailed
	case !certOK:
		out.Reason = ReasonCertificateMissing
	case cert.ExpiredAt(now):
		out.Reason = ReasonCertificateExpired
	default:
		out.Available = true
	}
	if !out.Available && dispoOK {
		out.Disposition = disposition
		out.DispoPresent = true
	}
	return out
}

func (b *Book) latestActiveDispositionLocked(valve string, now time.Time) (Disposition, bool) {
	for i := len(b.dispositions) - 1; i >= 0; i-- {
		d := b.dispositions[i]
		if d.Valve == valve && d.ActiveAt(now) {
			return d, true
		}
	}
	return Disposition{}, false
}

// InspectionCount 返回定检录入总数。
func (b *Book) InspectionCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.inspections)
}

// DispositionCount 返回替代处置记录总数。
func (b *Book) DispositionCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.dispositions)
}
