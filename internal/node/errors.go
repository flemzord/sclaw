// Package node implements device connectivity via WebSocket for the node manager.
package node

import "errors"

// Sentinel errors for the node package.
var (
	ErrNoDeviceAvailable = errors.New("node: no device with required capability is connected")
	ErrDeviceTimeout     = errors.New("node: device did not respond in time")
	ErrDeviceNotPaired   = errors.New("node: device is not paired")
	ErrInvalidToken      = errors.New("node: invalid pairing token")
	ErrMaxDevices        = errors.New("node: maximum number of devices reached")
)
