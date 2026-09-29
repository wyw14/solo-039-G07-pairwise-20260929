package relief

import (
	"context"
	"errors"
	"github.com/wyw14/solo-039/internal/model"
)

type Coordinator struct{ State *State }

type ShutdownResult struct {
	ReliefError   error
	SteamError    error
	Depressurized bool
	SteamClosed   bool
	Plan          Plan
}

func NewCoordinator() *Coordinator { return &Coordinator{State: New()} }

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

func CombineShutdown(reliefErr, steamErr error, reliefState model.Relief, steamClosed bool, plan Plan) ShutdownResult {
	return ShutdownResult{ReliefError: reliefErr, SteamError: steamErr, Depressurized: reliefErr == nil && reliefState.Released && !reliefState.TimedOut, SteamClosed: steamErr == nil && steamClosed, Plan: plan}
}

func (r ShutdownResult) Error() error {
	if r.ReliefError != nil {
		return r.ReliefError
	}
	return r.SteamError
}

func (r ShutdownResult) Safe() bool { return r.Depressurized && r.SteamClosed && r.Error() == nil }
