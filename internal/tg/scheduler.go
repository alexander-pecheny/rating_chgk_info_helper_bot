package tg

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Job is one recurring piece of work. A job with no interval runs once.
type Job struct {
	Name  string
	Next  time.Time
	Every time.Duration
	Run   func(context.Context)
}

// Scheduler runs the bot's periodic work. It replaces APScheduler, which was
// the only reason the Python bot needed a second event loop library.
type Scheduler struct {
	mu    sync.Mutex
	jobs  []*Job
	wake  chan struct{}
	clock func() time.Time
}

func NewScheduler() *Scheduler {
	return &Scheduler{wake: make(chan struct{}, 1), clock: time.Now}
}

func (s *Scheduler) Add(job *Job) {
	s.mu.Lock()
	s.jobs = append(s.jobs, job)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// RunSoon queues a one-off run, which is how the admin commands trigger a job
// by hand without blocking the reply.
func (s *Scheduler) RunSoon(name string, run func(context.Context)) {
	s.Add(&Job{Name: name, Next: s.clock(), Run: run})
}

// NextRuns lists when each scheduled job fires next, soonest first.
func (s *Scheduler) NextRuns() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	times := make([]time.Time, 0, len(s.jobs))
	for _, job := range s.jobs {
		times = append(times, job.Next)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	return times
}

// due removes and returns the jobs whose time has come, and reports when to
// look again.
func (s *Scheduler) due(now time.Time) ([]*Job, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ready []*Job
	remaining := s.jobs[:0]
	var earliest time.Time
	for _, job := range s.jobs {
		if !job.Next.After(now) {
			ready = append(ready, job)
			if job.Every == 0 {
				continue
			}
			job.Next = job.Next.Add(job.Every)
			// A job that fell far behind, typically because the machine slept,
			// catches up to the next slot rather than replaying every missed one.
			for !job.Next.After(now) {
				job.Next = job.Next.Add(job.Every)
			}
		}
		remaining = append(remaining, job)
		if earliest.IsZero() || job.Next.Before(earliest) {
			earliest = job.Next
		}
	}
	s.jobs = remaining
	return ready, earliest
}

func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		for {
			ready, earliest := s.due(s.clock())
			for _, job := range ready {
				slog.Debug("running scheduled job", "job", job.Name)
				s.runGuarded(ctx, job)
			}
			wait := time.Minute
			if !earliest.IsZero() {
				wait = earliest.Sub(s.clock())
			}
			if wait <= 0 {
				wait = time.Millisecond
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-s.wake:
			case <-timer.C:
			}
			timer.Stop()
		}
	}()
}

// runGuarded keeps one job's panic from taking the scheduler down with it.
func (s *Scheduler) runGuarded(ctx context.Context, job *Job) {
	defer func() {
		if problem := recover(); problem != nil {
			slog.Error("scheduled job panicked", "job", job.Name, "panic", problem)
		}
	}()
	job.Run(ctx)
}
