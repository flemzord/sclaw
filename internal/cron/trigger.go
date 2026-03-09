package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Trigger provides manual execution and introspection of prompt crons.
// It is registered as a service ("cron.trigger") so the gateway can discover it.
type Trigger struct {
	mu   sync.RWMutex
	jobs map[string]*PromptJob // keyed by PromptCronDef.Name
}

// NewTrigger creates a Trigger.
func NewTrigger() *Trigger {
	return &Trigger{
		jobs: make(map[string]*PromptJob),
	}
}

// Register adds a prompt job to the trigger registry.
// If a job with the same cron name already exists, it is overwritten.
func (t *Trigger) Register(job *PromptJob) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.jobs[job.Def.Name] = job
}

// Info is a summary returned by List and Get.
type Info struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Schedule    string            `json:"schedule"`
	Enabled     bool              `json:"enabled"`
	AgentID     string            `json:"agent_id"`
	LastResult  *PromptCronResult `json:"last_result,omitempty"`
}

// List returns all registered prompt crons sorted by name.
func (t *Trigger) List() []Info {
	t.mu.RLock()
	defer t.mu.RUnlock()

	infos := make([]Info, 0, len(t.jobs))
	for _, job := range t.jobs {
		info := Info{
			Name:        job.Def.Name,
			Description: job.Def.Description,
			Schedule:    job.Def.Schedule,
			Enabled:     job.Def.Enabled,
			AgentID:     job.AgentID,
		}
		if result, err := LoadResult(job.DataDir, job.Def.Name); err == nil {
			info.LastResult = result
		}
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// Get returns info for a single cron by name.
func (t *Trigger) Get(name string) (*Info, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	job, ok := t.jobs[name]
	if !ok {
		return nil, false
	}

	info := &Info{
		Name:        job.Def.Name,
		Description: job.Def.Description,
		Schedule:    job.Def.Schedule,
		Enabled:     job.Def.Enabled,
		AgentID:     job.AgentID,
	}
	if result, err := LoadResult(job.DataDir, job.Def.Name); err == nil {
		info.LastResult = result
	}
	return info, true
}

// Trigger runs a prompt cron by name. It blocks until the job completes.
// The caller is responsible for running this in a goroutine if async is desired.
func (t *Trigger) Trigger(ctx context.Context, name string) error {
	t.mu.RLock()
	job, ok := t.jobs[name]
	t.mu.RUnlock()

	if !ok {
		return fmt.Errorf("prompt cron %q not found", name)
	}

	return job.Run(ctx)
}

// LoadResult reads the last-run result from disk.
func LoadResult(dataDir, name string) (*PromptCronResult, error) {
	path := filepath.Join(ResultsDir(dataDir), name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result PromptCronResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
