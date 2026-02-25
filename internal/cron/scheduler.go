package cron

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"
)

// Scheduler manages periodic job execution using cron expressions.
// Each job is protected by a per-job mutex to prevent parallel execution
// of the same job (uses TryLock — atomic, no race).
type Scheduler struct {
	mu     sync.Mutex
	cron   *cron.Cron
	jobs   []Job
	names  map[string]struct{}
	locks  map[string]*sync.Mutex
	logger *slog.Logger
	cancel context.CancelFunc
}

// NewScheduler creates a scheduler. Jobs must be registered before Start().
func NewScheduler(logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		names:  make(map[string]struct{}),
		locks:  make(map[string]*sync.Mutex),
		logger: logger,
	}
}

// RegisterJob adds a job to the scheduler. Must be called before Start().
// Returns an error if a job with the same name is already registered.
func (s *Scheduler) RegisterJob(j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := j.Name()
	if _, exists := s.names[name]; exists {
		return fmt.Errorf("cron: duplicate job name %q", name)
	}

	s.names[name] = struct{}{}
	s.locks[name] = &sync.Mutex{}
	s.jobs = append(s.jobs, j)
	return nil
}

// Start initializes the cron scheduler and begins executing registered jobs.
// Returns an error if any job has an invalid schedule expression.
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	s.cron = cron.New(cron.WithParser(parser))

	for _, j := range s.jobs {
		job := j // capture loop variable
		lock := s.locks[job.Name()]

		_, err := s.cron.AddFunc(job.Schedule(), func() {
			// TryLock is atomic — no race between check and acquire.
			// If the previous tick is still running, skip this one.
			if !lock.TryLock() {
				s.logger.Warn("cron: job still running, skipping tick",
					"job", job.Name(),
				)
				return
			}
			defer lock.Unlock()

			s.logger.Debug("cron: job started", "job", job.Name())
			if err := job.Run(ctx); err != nil {
				s.logger.Error("cron: job failed",
					"job", job.Name(),
					"error", err,
				)
			} else {
				s.logger.Debug("cron: job completed", "job", job.Name())
			}
		})
		if err != nil {
			cancel()
			return fmt.Errorf("cron: invalid schedule for job %q: %w", job.Name(), err)
		}
	}

	s.cron.Start()
	s.logger.Info("cron: scheduler started", "jobs", len(s.jobs))
	return nil
}

// Stop gracefully shuts down the scheduler, waiting for in-flight jobs.
func (s *Scheduler) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}
	if s.cron != nil {
		// Wait for running jobs to complete.
		<-s.cron.Stop().Done()
		s.logger.Info("cron: scheduler stopped")
	}
	return nil
}
