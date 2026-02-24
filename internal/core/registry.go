package core

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
)

var (
	modules   = make(map[string]ModuleInfo)
	modulesMu sync.RWMutex
)

// RegisterModule registers a module by instantiating it to read its ModuleInfo.
// It panics if a module with the same ID is already registered or if the
// module info is invalid. Intended to be called from init() functions.
func RegisterModule(instance Module) {
	info := instance.ModuleInfo()
	if info.ID == "" {
		panic("module ID must not be empty")
	}
	if info.New == nil {
		panic(fmt.Sprintf("module %s: New function must not be nil", info.ID))
	}

	modulesMu.Lock()
	defer modulesMu.Unlock()

	id := string(info.ID)
	if _, exists := modules[id]; exists {
		panic(fmt.Sprintf("module already registered: %s", id))
	}
	modules[id] = info
}

// GetModule returns the ModuleInfo for the given ID, or false if not found.
func GetModule(id string) (ModuleInfo, bool) {
	modulesMu.RLock()
	defer modulesMu.RUnlock()
	info, ok := modules[id]
	return info, ok
}

// GetModules returns all registered modules sorted by ID.
func GetModules() []ModuleInfo {
	modulesMu.RLock()
	defer modulesMu.RUnlock()

	result := make([]ModuleInfo, 0, len(modules))
	for _, info := range modules {
		result = append(result, info)
	}
	slices.SortFunc(result, func(a, b ModuleInfo) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return result
}

// GetModulesByNamespace returns all modules whose ID starts with the given
// namespace prefix (e.g., "channel" matches "channel.telegram", "channel.discord").
func GetModulesByNamespace(namespace string) []ModuleInfo {
	prefix := namespace + "."

	modulesMu.RLock()
	defer modulesMu.RUnlock()

	var result []ModuleInfo
	for id, info := range modules {
		if strings.HasPrefix(id, prefix) {
			result = append(result, info)
		}
	}
	slices.SortFunc(result, func(a, b ModuleInfo) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return result
}

// resetRegistry clears the registry. Only for testing.
func resetRegistry() {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	modules = make(map[string]ModuleInfo)
}
