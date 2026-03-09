package channel

import (
	"context"
	"fmt"
	"sync"

	"github.com/flemzord/sclaw/pkg/message"
)

// Dispatcher routes outbound messages to the correct registered channel.
// It implements router.ResponseSender so it can be injected directly into
// the router configuration.
type Dispatcher struct {
	mu       sync.RWMutex
	channels map[string]Channel
}

// NewDispatcher creates an empty Dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		channels: make(map[string]Channel),
	}
}

// Register adds a channel under the given name.
// Returns ErrDuplicateChannel if the name is already taken.
func (d *Dispatcher) Register(name string, ch Channel) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.channels[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateChannel, name)
	}
	d.channels[name] = ch
	return nil
}

// Get returns the channel registered under name, or false if none.
func (d *Dispatcher) Get(name string) (Channel, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ch, ok := d.channels[name]
	return ch, ok
}

// Send dispatches an outbound message to the channel identified by
// msg.Channel. It returns ErrNoChannel if no channel is registered
// under that name.
func (d *Dispatcher) Send(ctx context.Context, msg message.OutboundMessage) error {
	d.mu.RLock()
	ch, ok := d.channels[msg.Channel]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNoChannel, msg.Channel)
	}
	return ch.Send(ctx, msg)
}

// SendStream delivers a streaming response to the channel identified by
// msg.Channel. If the channel implements StreamingChannel and currently
// supports streaming, it delegates to StreamingChannel.SendStream and
// returns (true, nil) on success. If the channel does not support
// streaming, it returns (false, nil) so the caller can fall back to Send.
func (d *Dispatcher) SendStream(ctx context.Context, msg message.OutboundMessage, stream <-chan string) (bool, error) {
	d.mu.RLock()
	ch, ok := d.channels[msg.Channel]
	d.mu.RUnlock()

	if !ok {
		return false, fmt.Errorf("%w: %s", ErrNoChannel, msg.Channel)
	}

	sc, ok := ch.(StreamingChannel)
	if !ok || !sc.SupportsStreaming() {
		return false, nil
	}

	if err := sc.SendStream(ctx, msg, stream); err != nil {
		return false, err
	}
	return true, nil
}

// Channels returns the names of all registered channels.
func (d *Dispatcher) Channels() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	names := make([]string, 0, len(d.channels))
	for name := range d.channels {
		names = append(names, name)
	}
	return names
}
