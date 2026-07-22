package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"
)

// Profile records opt-in, low-overhead aggregate index timings and standard Go
// runtime artifacts. It never records source content or graph facts.
type Profile struct {
	dir       string
	startedAt time.Time
	mu        sync.Mutex
	phases    map[string]*phaseSample
	counters  map[string]int64
	cpu       *os.File
	trace     *os.File
}

type phaseSample struct {
	Count int64 `json:"count"`
	Total int64 `json:"total_ns"`
	Min   int64 `json:"min_ns"`
	Max   int64 `json:"max_ns"`
}

type profileSummary struct {
	StartedAt string                 `json:"started_at"`
	Duration  int64                  `json:"duration_ns"`
	Result    string                 `json:"result"`
	Phases    map[string]phaseSample `json:"phases"`
	Counters  map[string]int64       `json:"counters"`
	Stats     Stats                  `json:"stats"`
}

func startProfile(dir string, traceEnabled bool) (*Profile, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create profile directory: %w", err)
	}
	p := &Profile{dir: dir, startedAt: time.Now(), phases: map[string]*phaseSample{}, counters: map[string]int64{}}
	cpu, err := os.Create(filepath.Join(dir, "cpu.pprof"))
	if err != nil {
		return nil, err
	}
	p.cpu = cpu
	if err := pprof.StartCPUProfile(cpu); err != nil {
		cpu.Close()
		return nil, fmt.Errorf("start CPU profile: %w", err)
	}
	if traceEnabled {
		traceFile, err := os.Create(filepath.Join(dir, "trace.out"))
		if err != nil {
			pprof.StopCPUProfile()
			cpu.Close()
			return nil, err
		}
		p.trace = traceFile
		if err := trace.Start(traceFile); err != nil {
			traceFile.Close()
			pprof.StopCPUProfile()
			cpu.Close()
			return nil, fmt.Errorf("start execution trace: %w", err)
		}
	}
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)
	return p, nil
}

func (p *Profile) Measure(name string, fn func() error) error {
	if p == nil {
		return fn()
	}
	started := time.Now()
	err := fn()
	p.Record(name, time.Since(started))
	return err
}

func (p *Profile) Record(name string, d time.Duration) {
	if p == nil {
		return
	}
	p.mu.Lock()
	s := p.phases[name]
	if s == nil {
		s = &phaseSample{Min: d.Nanoseconds()}
		p.phases[name] = s
	}
	ns := d.Nanoseconds()
	s.Count++
	s.Total += ns
	if ns < s.Min {
		s.Min = ns
	}
	if ns > s.Max {
		s.Max = ns
	}
	p.mu.Unlock()
}

func (p *Profile) Add(name string, value int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.counters[name] += value
	p.mu.Unlock()
}

func (p *Profile) Stop(stats Stats, runErr error) error {
	if p == nil {
		return nil
	}
	if p.trace != nil {
		trace.Stop()
	}
	pprof.StopCPUProfile()
	if p.trace != nil {
		_ = p.trace.Close()
	}
	_ = p.cpu.Close()
	runtime.SetBlockProfileRate(0)
	runtime.SetMutexProfileFraction(0)
	runtime.GC()
	for _, entry := range []struct{ name, file string }{
		{"heap", "heap.pprof"}, {"mutex", "mutex.pprof"}, {"block", "block.pprof"},
	} {
		f, err := os.Create(filepath.Join(p.dir, entry.file))
		if err != nil {
			return err
		}
		if prof := pprof.Lookup(entry.name); prof != nil {
			if err := prof.WriteTo(f, 0); err != nil {
				_ = f.Close()
				return err
			}
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	p.mu.Lock()
	phases := make(map[string]phaseSample, len(p.phases))
	for name, sample := range p.phases {
		phases[name] = *sample
	}
	counters := make(map[string]int64, len(p.counters))
	for name, value := range p.counters {
		counters[name] = value
	}
	p.mu.Unlock()
	result := "completed"
	if runErr != nil {
		result = runErr.Error()
	}
	data, err := json.MarshalIndent(profileSummary{StartedAt: p.startedAt.UTC().Format(time.RFC3339Nano), Duration: time.Since(p.startedAt).Nanoseconds(), Result: result, Phases: phases, Counters: counters, Stats: stats}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.dir, "summary.json"), append(data, '\n'), 0o644)
}
