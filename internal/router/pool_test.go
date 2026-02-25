package router

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flemzord/sclaw/pkg/message"
)

func TestWorkerPool_Size(t *testing.T) {
	t.Parallel()

	const workerCount = 3
	pool := NewWorkerPool(workerCount)
	inbox := make(chan envelope, workerCount)

	// Track how many messages are being processed concurrently.
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	var processed sync.WaitGroup
	processed.Add(workerCount)

	// barrier ensures all workers are running concurrently before any finishes.
	barrier := make(chan struct{})

	pool.Start(context.Background(), inbox, func(_ context.Context, _ envelope) {
		cur := concurrent.Add(1)
		// Track maximum concurrency observed.
		for {
			prev := maxConcurrent.Load()
			if cur <= prev || maxConcurrent.CompareAndSwap(prev, cur) {
				break
			}
		}
		<-barrier
		concurrent.Add(-1)
		processed.Done()
	})

	// Send exactly workerCount messages.
	for i := range workerCount {
		inbox <- envelope{
			Key: SessionKey{ChatID: "chat", ThreadID: string(rune('0' + i))},
			Message: message.InboundMessage{
				ID: string(rune('0' + i)),
			},
		}
	}

	// Wait briefly for workers to pick up all messages.
	time.Sleep(50 * time.Millisecond)

	// Release all workers.
	close(barrier)

	// Wait for all processing to complete.
	processed.Wait()

	if got := maxConcurrent.Load(); got < int32(workerCount) {
		t.Errorf("maxConcurrent = %d, want >= %d", got, workerCount)
	}

	close(inbox)
	pool.Wait()
}

func TestWorkerPool_Drain(t *testing.T) {
	t.Parallel()

	const msgCount = 5
	pool := NewWorkerPool(2)
	inbox := make(chan envelope, msgCount)

	var count atomic.Int32

	pool.Start(context.Background(), inbox, func(_ context.Context, _ envelope) {
		count.Add(1)
	})

	// Send messages and close inbox.
	for i := range msgCount {
		inbox <- envelope{
			Key: SessionKey{ChatID: "chat", ThreadID: string(rune('0' + i))},
			Message: message.InboundMessage{
				ID: string(rune('0' + i)),
			},
		}
	}
	close(inbox)

	// Wait should return after all messages are processed.
	pool.Wait()

	if got := count.Load(); got != msgCount {
		t.Errorf("processed %d messages, want %d", got, msgCount)
	}
}

func TestWorkerPool_DefaultSize(t *testing.T) {
	t.Parallel()

	pool := NewWorkerPool(0)
	if pool.size != DefaultWorkerCount {
		t.Errorf("size = %d, want %d for size <= 0", pool.size, DefaultWorkerCount)
	}

	poolNeg := NewWorkerPool(-5)
	if poolNeg.size != DefaultWorkerCount {
		t.Errorf("size = %d, want %d for negative size", poolNeg.size, DefaultWorkerCount)
	}
}
