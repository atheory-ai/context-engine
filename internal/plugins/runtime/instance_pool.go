package runtime

import (
	"context"
	"fmt"
	"sync"

	extism "github.com/extism/go-sdk"
)

// pluginInstancePool provides independent Extism instances for index-time
// language extraction. Extism instances are not goroutine-safe, so a checked
// out instance is used by one worker at a time. Production plugins share an
// Extism CompiledPlugin, so expanding the pool does not recompile their WASM.
type pluginInstancePool struct {
	base      *pluginInstance
	available chan *pluginInstance

	mu        sync.Mutex
	instances []*pluginInstance
	created   int
	closed    bool
	max       int
}

func newPluginInstancePool(base *pluginInstance, poolSize int) *pluginInstancePool {
	if poolSize < 1 {
		poolSize = 1
	}
	// Javy's development stream convention constructs a one-shot WASI module
	// per call, so it cannot safely share Extism instances. Keep it serial;
	// production Extism plugins receive the scalable pool below.
	if base.callConvention() == callConventionJavyStreamIO {
		poolSize = 1
	}
	p := &pluginInstancePool{
		base:      base,
		available: make(chan *pluginInstance, poolSize),
		instances: []*pluginInstance{base},
		created:   1,
		max:       poolSize,
	}
	p.available <- base
	return p
}

func (p *pluginInstancePool) withInstance(ctx context.Context, fn func(*pluginInstance) error) error {
	instance, err := p.acquire(ctx)
	if err != nil {
		return err
	}
	defer p.release(instance)
	return fn(instance)
}

func (p *pluginInstancePool) acquire(ctx context.Context) (*pluginInstance, error) {
	select {
	case instance := <-p.available:
		return instance, nil
	default:
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("plugin instance pool closed")
	}
	if p.created < p.max {
		p.created++
		p.mu.Unlock()
		instance, err := p.clone()
		if err != nil {
			p.mu.Lock()
			p.created--
			p.mu.Unlock()
			return nil, err
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			if closeErr := instance.closeDirect(); closeErr != nil {
				return nil, fmt.Errorf("close abandoned plugin instance: %w", closeErr)
			}
			return nil, fmt.Errorf("plugin instance pool closed")
		}
		p.instances = append(p.instances, instance)
		p.mu.Unlock()
		return instance, nil
	}
	p.mu.Unlock()

	select {
	case instance := <-p.available:
		return instance, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *pluginInstancePool) release(instance *pluginInstance) {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		if err := instance.closeDirect(); err != nil {
			return
		}
		return
	}
	p.available <- instance
}

func (p *pluginInstancePool) clone() (*pluginInstance, error) {
	if p.base.compiled == nil {
		return nil, fmt.Errorf("plugin %s does not have a reusable compiled instance", p.base.name)
	}
	wasm, err := p.base.compiled.Instance(context.Background(), extism.PluginInstanceConfig{})
	if err != nil {
		return nil, fmt.Errorf("create plugin extraction instance: %w", err)
	}
	return &pluginInstance{
		id:        p.base.id,
		name:      p.base.name,
		version:   p.base.version,
		wasm:      wasm,
		manifest:  p.base.manifest,
		wasmDir:   p.base.wasmDir,
		wasmBytes: p.base.wasmBytes,
		hostFuncs: p.base.hostFuncs,
		config:    p.base.config,
		exports:   p.base.exports,
	}, nil
}

func (p *pluginInstancePool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	instances := append([]*pluginInstance(nil), p.instances...)
	p.mu.Unlock()

	var firstErr error
	for _, instance := range instances {
		if err := instance.closeDirect(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if p.base.compiled != nil {
		if err := p.base.compiled.Close(context.Background()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Trim replaces every extraction instance after a completed bulk index. A
// warm base may itself have grown to the largest bulk-run payload, so retaining
// it defeats memory reclamation for a long-lived watcher. The replacement is a
// fresh, small instance from the already-compiled module; it stays ready for
// the next file change without retaining the bulk high-water linear memory.
func (p *pluginInstancePool) Trim() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	compiled := p.base.compiled
	p.mu.Unlock()
	if compiled == nil {
		return fmt.Errorf("plugin %s does not have a reusable compiled instance", p.base.name)
	}
	fresh, err := compiled.Instance(context.Background(), extism.PluginInstanceConfig{})
	if err != nil {
		return fmt.Errorf("create trimmed plugin instance: %w", err)
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fresh.Close(context.Background())
	}
	instances := append([]*pluginInstance(nil), p.instances[1:]...)
	p.base.mu.Lock()
	oldBase := p.base.wasm
	p.base.wasm = fresh
	p.base.mu.Unlock()
	p.instances = p.instances[:1]
	p.created = 1
	p.available = make(chan *pluginInstance, p.max)
	p.available <- p.base
	p.mu.Unlock()
	var firstErr error
	if err := oldBase.Close(context.Background()); err != nil {
		firstErr = err
	}
	for _, instance := range instances {
		if err := instance.closeDirect(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
