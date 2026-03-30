package lease

import "time"

type State struct {
	Name      string
	Namespace string
	Holder    string
	TTL       int32
	RenewTime time.Time
}

type Manager struct {
	store Store
}

func NewManager(store Store) *Manager {
	return &Manager{store: store}
}
