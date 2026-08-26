package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"minieureka/internal/model"
	"minieureka/internal/service"
)

type Options struct {
	Enabled        bool
	Seed           bool
	InstanceCount  int
	RandomSeed     int64
	HeartbeatEvery time.Duration
	Logger         *slog.Logger
}

type lease struct {
	service    string
	instanceID string
	leaseID    string
}

type Manager struct {
	service *service.Service
	opts    Options
	mu      sync.RWMutex
	leases  map[string]lease
}

func New(managerService *service.Service, options Options) *Manager {
	if options.InstanceCount < 0 {
		options.InstanceCount = 0
	}
	if options.HeartbeatEvery <= 0 {
		options.HeartbeatEvery = 10 * time.Second
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	manager := &Manager{opts: options, leases: make(map[string]lease)}
	if options.Seed {
		manager.service = managerService
	}
	return manager
}

func (m *Manager) Run(ctx context.Context) error {
	if !m.opts.Enabled {
		<-ctx.Done()
		return nil
	}
	if m.opts.Seed {
		if err := m.seed(); err != nil {
			return err
		}
	}
	ticker := time.NewTicker(m.opts.HeartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.heartbeat()
		}
	}
}

func (m *Manager) seed() error {
	services := []string{"orders", "catalog", "payments", "inventory", "users", "shipping", "gateway", "notifications", "search", "billing"}
	rng := rand.New(rand.NewSource(m.opts.RandomSeed))
	for index := range m.opts.InstanceCount {
		serviceName := services[index%len(services)]
		instanceID := fmt.Sprintf("%s-%04d", serviceName, index+1)
		result, err := m.service.Register(service.RegisterRequest{
			Service: serviceName, InstanceID: instanceID, RegistrationID: "demo-registration-" + instanceID,
			Host: fmt.Sprintf("10.42.%d.%d", (index/240)%200+1, index%240+10), Port: 8000 + index%1000,
			Protocol: model.ProtocolHTTP, Metadata: map[string]string{"zone": fmt.Sprintf("zone-%d", index%3+1), "version": fmt.Sprintf("1.%d.%d", rng.Intn(5), rng.Intn(10))}, Demo: true,
		})
		if err != nil {
			return fmt.Errorf("seed demo instance %s: %w", instanceID, err)
		}
		// Keep most instances active. A small deterministic tail demonstrates
		// DELAYED/TTL_EXPIRED without synthetic status writes.
		if index%20 == 0 {
			continue
		}
		m.mu.Lock()
		m.leases[result.Record.Key().String()] = lease{service: serviceName, instanceID: instanceID, leaseID: result.Record.LeaseID}
		m.mu.Unlock()
	}
	return nil
}

func (m *Manager) heartbeat() {
	m.mu.RLock()
	leases := make([]lease, 0, len(m.leases))
	for _, item := range m.leases {
		leases = append(leases, item)
	}
	m.mu.RUnlock()
	for _, item := range leases {
		operationID := fmt.Sprintf("demo-heartbeat-%s-%d", item.instanceID, time.Now().UnixNano())
		if _, err := m.service.Heartbeat(item.service, item.instanceID, item.leaseID, operationID); err != nil {
			if errors.Is(err, service.ErrStaleLease) || errors.Is(err, service.ErrNotFound) {
				m.mu.Lock()
				delete(m.leases, model.Key{Service: item.service, InstanceID: item.instanceID}.String())
				m.mu.Unlock()
				continue
			}
			m.opts.Logger.Warn("demo heartbeat failed", "service", item.service, "instance_id", item.instanceID, "error", err)
		}
	}
}

func (m *Manager) Offline(serviceName, instanceID, leaseID, operationID string) (service.OperationResult, error) {
	if !m.opts.Enabled {
		return service.OperationResult{}, service.ErrNotDemo
	}
	key := model.Key{Service: serviceName, InstanceID: instanceID}.String()
	m.mu.Lock()
	delete(m.leases, key)
	m.mu.Unlock()
	return m.service.Deregister(serviceName, instanceID, leaseID, operationID, true)
}

func (m *Manager) Managed() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.leases)
}
