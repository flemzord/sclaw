package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/tool"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Manager{})
}

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultMaxDevices        = 10
	defaultToolTimeout       = 30 * time.Second
	pairReadTimeout          = 10 * time.Second
	maxMissedHeartbeats      = 3
)

// ManagerConfig holds YAML configuration for the node manager module.
type ManagerConfig struct {
	PairingTokens     []string `yaml:"pairing_tokens"`
	HeartbeatInterval string   `yaml:"heartbeat_interval"`
	MaxDevices        int      `yaml:"max_devices"`
	ToolTimeout       string   `yaml:"tool_timeout"`
}

// defaults fills zero values with sensible defaults.
func (c *ManagerConfig) defaults() {
	if c.HeartbeatInterval == "" {
		c.HeartbeatInterval = "30s"
	}
	if c.MaxDevices <= 0 {
		c.MaxDevices = defaultMaxDevices
	}
	if c.ToolTimeout == "" {
		c.ToolTimeout = "30s"
	}
}

// Manager manages WebSocket connections to remote devices and exposes
// their capabilities as tools. It implements core.Module and related lifecycle
// interfaces.
type Manager struct {
	config            ManagerConfig
	appCtx            *core.AppContext
	logger            *slog.Logger
	store             *DeviceStore
	tokens            map[string]struct{}
	heartbeatInterval time.Duration
	toolTimeout       time.Duration
	cancel            context.CancelFunc
}

// ModuleInfo implements core.Module.
func (m *Manager) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "node.manager",
		New: func() core.Module { return &Manager{} },
	}
}

// Configure implements core.Configurable.
func (m *Manager) Configure(node *yaml.Node) error {
	if err := node.Decode(&m.config); err != nil {
		return err
	}
	m.config.defaults()
	return nil
}

// Provision implements core.Provisioner.
func (m *Manager) Provision(ctx *core.AppContext) error {
	m.appCtx = ctx
	m.logger = ctx.Logger
	m.store = NewDeviceStore()

	var err error
	m.heartbeatInterval, err = time.ParseDuration(m.config.HeartbeatInterval)
	if err != nil {
		return fmt.Errorf("node: invalid heartbeat_interval %q: %w", m.config.HeartbeatInterval, err)
	}

	m.toolTimeout, err = time.ParseDuration(m.config.ToolTimeout)
	if err != nil {
		return fmt.Errorf("node: invalid tool_timeout %q: %w", m.config.ToolTimeout, err)
	}

	m.tokens = make(map[string]struct{}, len(m.config.PairingTokens))
	for _, t := range m.config.PairingTokens {
		m.tokens[t] = struct{}{}
	}

	ctx.RegisterService("node.store", m.store)
	ctx.RegisterService("node.handler", http.HandlerFunc(m.handleWebSocket))

	return nil
}

// Validate implements core.Validator.
func (m *Manager) Validate() error {
	if len(m.tokens) == 0 {
		return errors.New("node: at least one pairing_token is required")
	}
	return nil
}

// Start implements core.Starter. It launches the heartbeat monitoring goroutine.
func (m *Manager) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	go m.heartbeatLoop(ctx)

	m.logger.Info("node manager started",
		"heartbeat_interval", m.heartbeatInterval,
		"max_devices", m.config.MaxDevices,
	)
	return nil
}

// Stop implements core.Stopper. It cancels background work and closes all
// device connections.
func (m *Manager) Stop(_ context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}

	m.store.Range(func(_ string, d *Device) bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.conn != nil {
			_ = d.conn.Close(websocket.StatusGoingAway, "server shutting down")
		}
		d.State = StateDisconnected
		return true
	})

	m.logger.Info("node manager stopped")
	return nil
}

// handleWebSocket is the HTTP handler for device WebSocket connections.
// It runs the full connection lifecycle: pair -> capabilities -> read loop.
func (m *Manager) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		m.logger.Error("websocket accept failed", "error", err)
		return
	}
	defer func() {
		_ = conn.Close(websocket.StatusInternalError, "unexpected close")
	}()

	device := &Device{
		State:           StateConnected,
		ConnectedAt:     time.Now(),
		LastSeenAt:      time.Now(),
		conn:            conn,
		pendingRequests: make(map[string]chan ToolResponse),
	}

	// Phase 1: read pair_request with timeout.
	if err := m.handlePairing(r.Context(), conn, device); err != nil {
		m.logger.Warn("pairing failed", "error", err)
		return
	}

	// Phase 2: read capabilities declaration.
	if err := m.handleCapabilities(r.Context(), conn, device); err != nil {
		m.logger.Warn("capabilities exchange failed", "device", device.ID, "error", err)
		m.store.Remove(device.ID)
		return
	}

	m.logger.Info("device paired",
		"device_id", device.ID,
		"name", device.Name,
		"platform", device.Platform,
		"capabilities", device.Capabilities,
	)

	// Phase 3: read loop.
	m.readLoop(r.Context(), conn, device)

	// Cleanup on disconnect.
	device.mu.Lock()
	device.State = StateDisconnected
	device.mu.Unlock()
	m.store.Remove(device.ID)
	m.logger.Info("device disconnected", "device_id", device.ID)
}

func (m *Manager) handlePairing(ctx context.Context, conn *websocket.Conn, device *Device) error {
	pairCtx, cancel := context.WithTimeout(ctx, pairReadTimeout)
	defer cancel()

	_, data, err := conn.Read(pairCtx)
	if err != nil {
		m.sendError(ctx, conn, "", "failed to read pair request")
		return fmt.Errorf("read pair_request: %w", err)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		m.sendError(ctx, conn, "", "invalid message format")
		return fmt.Errorf("unmarshal envelope: %w", err)
	}

	if env.Type != MsgPairRequest {
		m.sendError(ctx, conn, env.ID, "expected pair_request")
		return fmt.Errorf("unexpected message type: %s", env.Type)
	}

	var req PairRequest
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		m.sendError(ctx, conn, env.ID, "invalid pair_request payload")
		return fmt.Errorf("unmarshal pair_request: %w", err)
	}

	// Validate token.
	if _, ok := m.tokens[req.Token]; !ok {
		m.sendPairResponse(ctx, conn, env.ID, false, "", "invalid pairing token")
		return ErrInvalidToken
	}

	deviceID, err := generateDeviceID()
	if err != nil {
		m.sendError(ctx, conn, env.ID, "internal error")
		return fmt.Errorf("generate device ID: %w", err)
	}

	device.ID = deviceID
	device.Name = req.DeviceName
	device.Platform = req.Platform

	// Atomically check max devices and add in one operation to avoid TOCTOU race.
	if !m.store.AddIfUnder(device, m.config.MaxDevices) {
		m.sendPairResponse(ctx, conn, env.ID, false, "", "maximum number of devices reached")
		return ErrMaxDevices
	}

	m.sendPairResponse(ctx, conn, env.ID, true, deviceID, "")

	return nil
}

func (m *Manager) handleCapabilities(ctx context.Context, conn *websocket.Conn, device *Device) error {
	capCtx, cancel := context.WithTimeout(ctx, pairReadTimeout)
	defer cancel()

	_, data, err := conn.Read(capCtx)
	if err != nil {
		return fmt.Errorf("read capabilities: %w", err)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("unmarshal envelope: %w", err)
	}

	if env.Type != MsgCapabilities {
		return fmt.Errorf("expected capabilities, got %s", env.Type)
	}

	var decl CapabilitiesDeclaration
	if err := json.Unmarshal(env.Payload, &decl); err != nil {
		return fmt.Errorf("unmarshal capabilities: %w", err)
	}

	device.mu.Lock()
	device.Capabilities = decl.Capabilities
	device.State = StatePaired
	device.mu.Unlock()

	return nil
}

func (m *Manager) readLoop(ctx context.Context, conn *websocket.Conn, device *Device) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			m.logger.Warn("invalid message from device", "device_id", device.ID, "error", err)
			continue
		}

		device.mu.Lock()
		device.LastSeenAt = time.Now()
		device.mu.Unlock()

		switch env.Type {
		case MsgHeartbeat:
			m.sendHeartbeatAck(ctx, conn, env.ID)

		case MsgToolResponse:
			var resp ToolResponse
			if err := json.Unmarshal(env.Payload, &resp); err != nil {
				m.logger.Warn("invalid tool_response", "device_id", device.ID, "error", err)
				continue
			}
			device.mu.Lock()
			if ch, ok := device.pendingRequests[env.ID]; ok {
				// Non-blocking send: drop duplicate/late responses to avoid
				// blocking the read loop while holding device.mu.
				select {
				case ch <- resp:
				default:
				}
			}
			device.mu.Unlock()

		default:
			m.logger.Warn("unexpected message type in read loop",
				"device_id", device.ID,
				"type", env.Type,
			)
		}
	}
}

func (m *Manager) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(m.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkHeartbeats()
		}
	}
}

func (m *Manager) checkHeartbeats() {
	now := time.Now()
	threshold := m.heartbeatInterval * maxMissedHeartbeats

	m.store.Range(func(_ string, d *Device) bool {
		d.mu.Lock()
		defer d.mu.Unlock()

		if d.State != StatePaired {
			return true
		}

		if now.Sub(d.LastSeenAt) > threshold {
			m.logger.Warn("device heartbeat timeout, disconnecting",
				"device_id", d.ID,
				"last_seen", d.LastSeenAt,
			)
			if d.conn != nil {
				_ = d.conn.Close(websocket.StatusGoingAway, "heartbeat timeout")
			}
			d.State = StateDisconnected
			// Remove outside lock via deferred cleanup — the read loop will
			// handle removal when it detects the closed connection.
		}

		return true
	})
}

// sendEnvelope marshals and writes an Envelope to the connection.
func (m *Manager) sendEnvelope(ctx context.Context, conn *websocket.Conn, env Envelope) {
	data, err := json.Marshal(env)
	if err != nil {
		m.logger.Error("marshal envelope failed", "error", err)
		return
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		m.logger.Warn("write envelope failed", "error", err)
	}
}

func (m *Manager) sendError(ctx context.Context, conn *websocket.Conn, id, message string) {
	payload, _ := json.Marshal(map[string]string{"message": message})
	m.sendEnvelope(ctx, conn, Envelope{
		Type:      MsgError,
		ID:        id,
		Payload:   payload,
		Timestamp: time.Now(),
	})
}

func (m *Manager) sendPairResponse(ctx context.Context, conn *websocket.Conn, id string, accepted bool, deviceID, reason string) {
	resp := PairResponse{
		Accepted: accepted,
		DeviceID: deviceID,
		Reason:   reason,
	}
	payload, _ := json.Marshal(resp)
	m.sendEnvelope(ctx, conn, Envelope{
		Type:      MsgPairResponse,
		ID:        id,
		Payload:   payload,
		Timestamp: time.Now(),
	})
}

func (m *Manager) sendHeartbeatAck(ctx context.Context, conn *websocket.Conn, id string) {
	m.sendEnvelope(ctx, conn, Envelope{
		Type:      MsgHeartbeatAck,
		ID:        id,
		Timestamp: time.Now(),
	})
}

// RegisterDeviceTools creates and registers device tools in the given registry.
func (m *Manager) RegisterDeviceTools(registry *tool.Registry) error {
	tools := []struct {
		name        string
		description string
		capability  Capability
	}{
		{"node.camera.snap", "Take a photo using the connected device's camera", CapCamera},
		{"node.screen.capture", "Capture a screenshot from the connected device", CapScreen},
		{"node.location", "Get the current GPS location of the connected device", CapLocation},
		{"node.notification", "Send a notification to the connected device", CapNotification},
		{"node.clipboard.read", "Read the clipboard content from the connected device", CapClipboard},
	}

	for _, t := range tools {
		dt := &DeviceTool{
			name:        t.name,
			description: t.description,
			capability:  t.capability,
			schema:      json.RawMessage(`{}`),
			store:       m.store,
			timeout:     m.toolTimeout,
		}
		if err := registry.Register(dt); err != nil {
			return fmt.Errorf("register tool %s: %w", t.name, err)
		}
	}

	return nil
}
