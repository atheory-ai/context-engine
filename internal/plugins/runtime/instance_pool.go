package runtime

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	extism "github.com/extism/go-sdk"
)

// pluginInstancePool provides independent Extism instances for index-time
// language extraction. Extism instances are not goroutine-safe, so a checked
// out instance is used by one worker at a time. Compilation remains shared by
// the wazero cache carried in plugin.config.
type pluginInstancePool struct {
	base      *pluginInstance
	available chan *pluginInstance

	mu        sync.Mutex
	instances []*pluginInstance
	created   int
	closed    bool
	max       int
}

func newPluginInstancePool(base *pluginInstance) *pluginInstancePool {
	poolSize := runtime.NumCPU()
	if poolSize > 8 {
		poolSize = 8
	}
	if poolSize < 1 {
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
	wasm, err := extism.NewPlugin(context.Background(), newExtismManifest(p.base.wasmBytes), p.base.config, p.base.hostFuncs)
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
	return firstErr
}
