package relief

import (
	"context"
	"errors"
	"github.com/wyw14/solo-039/internal/model"
	"time"
)

type Coordinator struct {
	State *State
	Book  *Book
}

type ShutdownResult struct {
	ReliefError   error
	SteamError    error
	Depressurized bool
	SteamClosed   bool
	// ValveAvailable 表示泄压阀持有效投用证书；为 false 时 Reason 给出原因。
	ValveAvailable bool
	Reason         UnavailableReason
	// Disposition 为不可投用时实际依据的替代处置记录(可能为空)。
	Disposition *Disposition
}

func NewCoordinator() *Coordinator {
	return &Coordinator{State: New(), Book: NewBook()}
}

// PlanShutdown 在执行安全停机前核查泄压阀的投用前定检依据。
func (c *Coordinator) PlanShutdown(valve string, now time.Time) Availability {
	return c.Book.CheckValve(valve, now)
}

func (c *Coordinator) Shutdown(ctx context.Context, cpg model.Campaign, valve string, timeout bool) error {
	c.State.Begin(valve)
	if timeout {
		c.State.Timeout()
		return errors.New("relief valve timeout")
	}
	c.State.Release()
	return nil
}

func (c *Coordinator) Safe() bool { return c.State.Snapshot().Released && !c.State.Snapshot().TimedOut }

func CombineShutdown(reliefErr, steamErr error, reliefState model.Relief, steamClosed bool, availability Availability) ShutdownResult {
	result := ShutdownResult{
		ReliefError:    reliefErr,
		SteamError:     steamErr,
		Depressurized:  reliefErr == nil && reliefState.Released && !reliefState.TimedOut,
		SteamClosed:    steamErr == nil && steamClosed,
		ValveAvailable: availability.Available,
		Reason:         availability.Reason,
	}
	if availability.DispoPresent {
		disposition := availability.Disposition
		result.Disposition = &disposition
	}
	return result
}

func (r ShutdownResult) Error() error {
	if r.ReliefError != nil {
		return r.ReliefError
	}
	return r.SteamError
}

func (r ShutdownResult) Safe() bool { return r.Depressurized && r.SteamClosed && r.Error() == nil }

// Certified 报告泄压阀是否持有效投用证书。
func (r ShutdownResult) Certified() bool { return r.ValveAvailable }

// CoveredByDisposition 报告泄压阀虽不可投用、但有在有效期内的替代处置记录。
func (r ShutdownResult) CoveredByDisposition() bool { return r.Disposition != nil }
