package router

import (
	"context"
	"sync"

	"github.com/flemzord/sclaw/pkg/message"
)

// DefaultWorkerCount is the number of workers when no size is specified.
const DefaultWorkerCount = 10

// envelope is the internal message wrapper for the worker pool inbox.
type envelope struct {
	Message message.InboundMessage
	Key     SessionKey
}

// WorkerPool manages a fixed set of goroutines that consume from the inbox.
type WorkerPool struct {
	size int
	wg   sync.WaitGroup
}

// NewWorkerPool creates a pool with the given size.
// If size <= 0, DefaultWorkerCount is used.
func NewWorkerPool(size int) *WorkerPool {
	if size <= 0 {
		size = DefaultWorkerCount
	}
	return &WorkerPool{size: size}
}

// Start launches worker goroutines that consume envelopes from inbox.
func (p *WorkerPool) Start(ctx context.Context, inbox <-chan envelope, handler func(context.Context, envelope)) {
	for range p.size {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for env := range inbox {
				handler(ctx, env)
			}
		}()
	}
}

// Wait blocks until all workers have exited.
func (p *WorkerPool) Wait() {
	p.wg.Wait()
}
