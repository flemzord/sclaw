package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// DeviceState represents the current connection state of a device.
type DeviceState string

// Device connection states.
const (
	StateConnected    DeviceState = "connected"
	StatePaired       DeviceState = "paired"
	StateDisconnected DeviceState = "disconnected"
)

// Device represents a connected remote device.
type Device struct {
	mu              sync.Mutex
	ID              string
	Name            string
	Platform        string
	State           DeviceState
	Capabilities    []Capability
	ConnectedAt     time.Time
	LastSeenAt      time.Time
	conn            *websocket.Conn
	pendingRequests map[string]chan ToolResponse
}

// SendToolRequest sends a tool invocation to the device and waits for the response.
func (d *Device) SendToolRequest(ctx context.Context, toolName string, args json.RawMessage) (ToolResponse, error) {
	d.mu.Lock()
	if d.State != StatePaired {
		d.mu.Unlock()
		return ToolResponse{}, fmt.Errorf("%w (state: %s)", ErrDeviceNotPaired, d.State)
	}

	correlationID, err := generateCorrelationID()
	if err != nil {
		d.mu.Unlock()
		return ToolResponse{}, err
	}

	ch := make(chan ToolResponse, 1)
	d.pendingRequests[correlationID] = ch
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.pendingRequests, correlationID)
		d.mu.Unlock()
	}()

	req := ToolRequest{ToolName: toolName, Arguments: args}
	payload, _ := json.Marshal(req)
	env := Envelope{
		Type:      MsgToolRequest,
		ID:        correlationID,
		Payload:   payload,
		Timestamp: time.Now(),
	}
	data, _ := json.Marshal(env)
	if err := d.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return ToolResponse{}, fmt.Errorf("node: write to device %s: %w", d.ID, err)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return ToolResponse{}, ctx.Err()
	}
}

// DeviceStore is a concurrent-safe in-memory store for connected devices.
type DeviceStore struct {
	mu      sync.RWMutex
	devices map[string]*Device
}

// NewDeviceStore creates an empty DeviceStore.
func NewDeviceStore() *DeviceStore {
	return &DeviceStore{devices: make(map[string]*Device)}
}

// Add registers a device in the store.
func (s *DeviceStore) Add(d *Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[d.ID] = d
}

// Get returns the device with the given ID, or false if not found.
func (s *DeviceStore) Get(id string) (*Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[id]
	return d, ok
}

// Remove deletes a device from the store.
func (s *DeviceStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices, id)
}

// Len returns the number of devices in the store.
func (s *DeviceStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.devices)
}

// ByCapability returns all paired devices that declare the given capability.
func (s *DeviceStore) ByCapability(capability Capability) []*Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Device
	for _, d := range s.devices {
		if d.State == StatePaired && slices.Contains(d.Capabilities, capability) {
			result = append(result, d)
		}
	}
	return result
}

// Range iterates over all devices, calling fn for each. If fn returns false,
// iteration stops.
func (s *DeviceStore) Range(fn func(id string, d *Device) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, d := range s.devices {
		if !fn(id, d) {
			return
		}
	}
}

func generateCorrelationID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func generateDeviceID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "dev-" + hex.EncodeToString(buf[:]), nil
}
