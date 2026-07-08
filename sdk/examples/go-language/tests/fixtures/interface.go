package store

import "context"

// Storage defines the storage interface.
type Storage interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
}

// MemoryStore implements Storage in memory.
type MemoryStore struct {
	data map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]string)}
}

func (m *MemoryStore) Get(_ context.Context, key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", nil
	}
	return v, nil
}

func (m *MemoryStore) Set(_ context.Context, key, value string) error {
	m.data[key] = value
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}
