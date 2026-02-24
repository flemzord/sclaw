package tool

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRequester struct {
	respondFunc func(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error)
}

func (f *fakeRequester) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	return f.respondFunc(ctx, req)
}

func TestPendingApproval_InitialState(t *testing.T) {
	t.Parallel()

	p := NewPendingApproval()
	if p.State() != StateIdle {
		t.Errorf("initial state: got %d, want %d (StateIdle)", p.State(), StateIdle)
	}
	if p.ResponseChan == nil {
		t.Fatal("ResponseChan should be initialized")
	}
}

func TestPendingApproval_Approved(t *testing.T) {
	t.Parallel()

	p := NewPendingApproval()
	requester := &fakeRequester{
		respondFunc: func(_ context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			return ApprovalResponse{Approved: true, Reason: "ok"}, nil
		},
	}

	resp, err := p.Begin(context.Background(), requester, ApprovalRequest{
		ID:       "test-1",
		ToolName: "read_file",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Approved {
		t.Error("expected approval")
	}
	if p.State() != StateIdle {
		t.Errorf("should return to idle, got %d", p.State())
	}
}

func TestPendingApproval_ApprovedViaResponseChan(t *testing.T) {
	t.Parallel()

	p := NewPendingApproval()

	go func() {
		time.Sleep(20 * time.Millisecond)
		p.ResponseChan <- ApprovalResponse{Approved: true, Reason: "inline approve"}
	}()

	resp, err := p.Begin(context.Background(), nil, ApprovalRequest{
		ID:       "test-inline-1",
		ToolName: "read_file",
	}, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Approved {
		t.Fatal("expected approval via ResponseChan")
	}
	if resp.Reason != "inline approve" {
		t.Fatalf("reason = %q, want %q", resp.Reason, "inline approve")
	}
}

func TestPendingApproval_Denied(t *testing.T) {
	t.Parallel()

	p := NewPendingApproval()
	requester := &fakeRequester{
		respondFunc: func(_ context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			return ApprovalResponse{Approved: false, Reason: "nope"}, nil
		},
	}

	resp, err := p.Begin(context.Background(), requester, ApprovalRequest{
		ID:       "test-2",
		ToolName: "exec_cmd",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Approved {
		t.Error("expected denial")
	}
	if resp.Reason != "nope" {
		t.Errorf("reason: got %q, want %q", resp.Reason, "nope")
	}
}

func TestPendingApproval_Timeout(t *testing.T) {
	t.Parallel()

	p := NewPendingApproval()
	requester := &fakeRequester{
		respondFunc: func(ctx context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			// Block until context cancels.
			<-ctx.Done()
			return ApprovalResponse{}, ctx.Err()
		},
	}

	resp, err := p.Begin(context.Background(), requester, ApprovalRequest{
		ID:       "test-3",
		ToolName: "exec_cmd",
	}, 50*time.Millisecond)

	if !errors.Is(err, ErrApprovalTimeout) {
		t.Errorf("expected ErrApprovalTimeout, got %v", err)
	}
	if resp.Approved {
		t.Error("timed-out approval should not be approved")
	}
}

func TestPendingApproval_CanceledContext(t *testing.T) {
	t.Parallel()

	p := NewPendingApproval()
	requester := &fakeRequester{
		respondFunc: func(ctx context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			<-ctx.Done()
			return ApprovalResponse{}, ctx.Err()
		},
	}

	parent, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := p.Begin(parent, requester, ApprovalRequest{
		ID:       "test-cancel",
		ToolName: "exec_cmd",
	}, 5*time.Second)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if resp.Approved {
		t.Error("canceled approval should not be approved")
	}
	if p.State() != StateIdle {
		t.Errorf("should return to idle, got %d", p.State())
	}
}

func TestPendingApproval_RequesterError(t *testing.T) {
	t.Parallel()

	p := NewPendingApproval()
	wantErr := errors.New("connection lost")
	requester := &fakeRequester{
		respondFunc: func(_ context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			return ApprovalResponse{}, wantErr
		},
	}

	_, err := p.Begin(context.Background(), requester, ApprovalRequest{
		ID:       "test-4",
		ToolName: "net_call",
	}, 5*time.Second)

	if !errors.Is(err, wantErr) {
		t.Errorf("expected %v, got %v", wantErr, err)
	}
	if p.State() != StateIdle {
		t.Errorf("should return to idle after error, got %d", p.State())
	}
}

func TestPendingApproval_RejectWhilePending(t *testing.T) {
	t.Parallel()

	p := NewPendingApproval()

	// A requester that blocks indefinitely.
	started := make(chan struct{})
	requester := &fakeRequester{
		respondFunc: func(ctx context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			close(started)
			<-ctx.Done()
			return ApprovalResponse{}, ctx.Err()
		},
	}

	go func() {
		_, _ = p.Begin(context.Background(), requester, ApprovalRequest{
			ID: "test-5",
		}, 2*time.Second)
	}()

	<-started // Wait for first Begin to reach pending state.

	// Second Begin should fail because already pending.
	resp, err := p.Begin(context.Background(), requester, ApprovalRequest{
		ID: "test-6",
	}, 1*time.Second)

	if !errors.Is(err, ErrDenied) {
		t.Errorf("concurrent Begin: expected ErrDenied, got %v", err)
	}
	if resp.Approved {
		t.Error("concurrent Begin should not be approved")
	}
}

func TestApprovalState_Constants(t *testing.T) {
	t.Parallel()

	if StateIdle != 0 {
		t.Errorf("StateIdle: got %d, want 0", StateIdle)
	}
	if StatePending != 1 {
		t.Errorf("StatePending: got %d, want 1", StatePending)
	}
	if StateTimeout != 2 {
		t.Errorf("StateTimeout: got %d, want 2", StateTimeout)
	}
}
