package node

import (
	"strings"
	"testing"
	"time"
)

func TestDeviceStore_AddGetRemove(t *testing.T) {
	t.Parallel()

	store := NewDeviceStore()

	d := &Device{
		ID:          "dev-abc",
		Name:        "test-device",
		State:       StatePaired,
		ConnectedAt: time.Now(),
	}

	store.Add(d)

	got, ok := store.Get("dev-abc")
	if !ok {
		t.Fatal("expected device to be found")
	}
	if got.ID != "dev-abc" {
		t.Errorf("ID = %q, want %q", got.ID, "dev-abc")
	}
	if got.Name != "test-device" {
		t.Errorf("Name = %q, want %q", got.Name, "test-device")
	}

	store.Remove("dev-abc")

	_, ok = store.Get("dev-abc")
	if ok {
		t.Error("expected device to be removed")
	}
}

func TestDeviceStore_ByCapability(t *testing.T) {
	t.Parallel()

	store := NewDeviceStore()

	// Paired device with camera.
	paired := &Device{
		ID:           "dev-1",
		State:        StatePaired,
		Capabilities: []Capability{CapCamera, CapLocation},
	}
	store.Add(paired)

	// Connected (not paired) device with camera — should be excluded.
	connected := &Device{
		ID:           "dev-2",
		State:        StateConnected,
		Capabilities: []Capability{CapCamera},
	}
	store.Add(connected)

	// Paired device without camera.
	noCam := &Device{
		ID:           "dev-3",
		State:        StatePaired,
		Capabilities: []Capability{CapClipboard},
	}
	store.Add(noCam)

	results := store.ByCapability(CapCamera)
	if len(results) != 1 {
		t.Fatalf("ByCapability(camera) returned %d devices, want 1", len(results))
	}
	if results[0].ID != "dev-1" {
		t.Errorf("device ID = %q, want %q", results[0].ID, "dev-1")
	}

	// No device has audio capability.
	audioDevices := store.ByCapability(CapAudio)
	if len(audioDevices) != 0 {
		t.Errorf("ByCapability(audio) returned %d devices, want 0", len(audioDevices))
	}
}

func TestDeviceStore_Range(t *testing.T) {
	t.Parallel()

	store := NewDeviceStore()
	store.Add(&Device{ID: "dev-a", State: StatePaired})
	store.Add(&Device{ID: "dev-b", State: StatePaired})
	store.Add(&Device{ID: "dev-c", State: StatePaired})

	var visited int
	store.Range(func(_ string, _ *Device) bool {
		visited++
		return visited < 2 // stop after 2
	})

	if visited != 2 {
		t.Errorf("visited = %d, want 2", visited)
	}
}

func TestDeviceStore_Len(t *testing.T) {
	t.Parallel()

	store := NewDeviceStore()
	if store.Len() != 0 {
		t.Errorf("Len() = %d, want 0", store.Len())
	}

	store.Add(&Device{ID: "dev-1"})
	store.Add(&Device{ID: "dev-2"})

	if store.Len() != 2 {
		t.Errorf("Len() = %d, want 2", store.Len())
	}

	store.Remove("dev-1")

	if store.Len() != 1 {
		t.Errorf("Len() = %d, want 1", store.Len())
	}
}

func TestGenerateDeviceID(t *testing.T) {
	t.Parallel()

	id, err := generateDeviceID()
	if err != nil {
		t.Fatalf("generateDeviceID: %v", err)
	}

	if id == "" {
		t.Fatal("expected non-empty device ID")
	}

	if !strings.HasPrefix(id, "dev-") {
		t.Errorf("device ID %q does not have 'dev-' prefix", id)
	}

	// Ensure uniqueness across calls.
	id2, err := generateDeviceID()
	if err != nil {
		t.Fatalf("generateDeviceID (2nd): %v", err)
	}
	if id == id2 {
		t.Errorf("two generated IDs should differ: %q", id)
	}
}
