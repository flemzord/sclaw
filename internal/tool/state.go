package tool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ApprovalState represents the current state of a pending approval.
type ApprovalState int

// ApprovalState values for the pending approval state machine.
const (
	StateIdle    ApprovalState = iota // No pending approval
	StatePending                      // Waiting for user response
	StateTimeout                      // Timed out, denied by default
)

// PendingApproval manages the state machine for a single approval flow.
// It transitions: idle → pending → (response | timeout → deny-by-default).
type PendingApproval struct {
	mu           sync.Mutex
	state        ApprovalState
	ResponseChan chan ApprovalResponse
}

// NewPendingApproval creates a new PendingApproval in the idle state.
func NewPendingApproval() *PendingApproval {
	return &PendingApproval{
		state:        StateIdle,
		ResponseChan: make(chan ApprovalResponse, 1),
	}
}

// State returns the current approval state.
func (p *PendingApproval) State() ApprovalState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Begin starts an approval request with the given requester and timeout.
// It transitions from idle to pending, sends the request, and returns the
// response. On timeout, the approval is denied by default.
// Returns to idle after completion.
func (p *PendingApproval) Begin(
	ctx context.Context,
	requester ApprovalRequester,
	req ApprovalRequest,
	timeout time.Duration,
) (ApprovalResponse, error) {
	p.mu.Lock()
	if p.state != StateIdle {
		p.mu.Unlock()
		return ApprovalResponse{}, ErrDenied
	}
	p.state = StatePending
	if p.ResponseChan == nil {
		p.ResponseChan = make(chan ApprovalResponse, 1)
	}
	respCh := p.ResponseChan
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.state = StateIdle
		p.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Drop any stale response from a previous flow.
	select {
	case <-respCh:
	default:
	}

	requestErrCh := make(chan error, 1)
	if requester != nil {
		go func() {
			resp, err := requester.RequestApproval(ctx, req)
			if err != nil {
				requestErrCh <- err
				return
			}
			select {
			case respCh <- resp:
			case <-ctx.Done():
			}
		}()
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case err := <-requestErrCh:
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			p.mu.Lock()
			p.state = StateTimeout
			p.mu.Unlock()
			timeoutResp := ApprovalResponse{Approved: false, Reason: "timed out"}
			select {
			case respCh <- timeoutResp:
			default:
			}
			return timeoutResp, ErrApprovalTimeout
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return ApprovalResponse{}, ctx.Err()
		}
		return ApprovalResponse{}, err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			p.mu.Lock()
			p.state = StateTimeout
			p.mu.Unlock()
			timeoutResp := ApprovalResponse{Approved: false, Reason: "timed out"}
			select {
			case respCh <- timeoutResp:
			default:
			}
			return timeoutResp, ErrApprovalTimeout
		}
		return ApprovalResponse{}, ctx.Err()
	}
}
