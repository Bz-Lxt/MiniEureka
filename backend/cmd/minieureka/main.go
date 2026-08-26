package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"minieureka/internal/api"
	"minieureka/internal/clock"
	"minieureka/internal/cluster"
	"minieureka/internal/config"
	"minieureka/internal/demo"
	"minieureka/internal/events"
	"minieureka/internal/gossip"
	"minieureka/internal/model"
	"minieureka/internal/observe"
	"minieureka/internal/registry"
	"minieureka/internal/service"
	"minieureka/internal/ttl"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger := observe.NewLogger(os.Stdout, cfg.LogLevel)
	slog.SetDefault(logger)
	hlc, err := clock.New(cfg.NodeID, clock.WithMaxFutureSkew(cfg.MaxClockSkew))
	if err != nil {
		return err
	}
	store, err := registry.New(cfg.ShardCount)
	if err != nil {
		return err
	}
	eventRing := events.New(cfg.EventCapacity, cfg.NodeID, cfg.BootID)
	self := model.Member{
		NodeID: cfg.NodeID, BootID: cfg.BootID, HTTPAddress: cfg.HTTPAdvertiseAddr,
		GossipAddress: cfg.GossipAdvertiseAddr, Status: model.MemberAlive, Incarnation: 1,
		LastSeenAt: time.Now().UTC(), Version: hlc.Now(),
	}
	members := cluster.NewTable(self, hlc, cfg.SuspicionTimeout, cfg.DeadTimeout, func(member model.Member) {
		eventRing.Publish(events.Event{Type: events.MemberChanged, EntityKey: member.NodeID, Revision: member.Version.String(), OriginNodeID: member.Version.OriginNodeID, Payload: events.Payload(map[string]any{"member": member})})
	})
	readiness := &observe.Readiness{}
	metrics := observe.NewMetrics(func() observe.InstanceCounts {
		counts := store.Counts()
		return observe.InstanceCounts{Active: counts.Active, Delayed: counts.Delayed, Evicted: counts.Evicted}
	}, func() map[string]int {
		result := map[string]int{"ALIVE": 0, "SUSPECT": 0, "DEAD": 0}
		for _, member := range members.Snapshot() {
			result[string(member.Status)]++
		}
		return result
	}, eventRing.Dropped)
	var registryService *service.Service
	wheel := ttl.New(512, cfg.TickInterval, time.Now, func(tasks []ttl.Task) {
		registryService.HandleTasks(tasks)
	})
	registryService = service.New(store, hlc, eventRing, wheel, service.Options{
		NodeID: cfg.NodeID, LeaseTTL: cfg.LeaseTTL, DelayedAfter: cfg.DelayedAfter,
		TombstoneTTL: cfg.TombstoneTTL, EvictedDisplayTTL: cfg.EvictedDisplayTTL,
	})
	faults := gossip.NewFaults(cfg.DemoRandomSeed)
	auth := gossip.NewAuthenticator(cfg.GossipSecret, cfg.ClusterID, cfg.MaxClockSkew, cfg.MaxUDPBytes)
	transport := gossip.NewUDPTransport(cfg.GossipAddr, cfg.MaxUDPBytes, faults)
	engine := gossip.NewEngine(gossip.EngineConfig{
		NodeID: cfg.NodeID, AdvertiseAddress: cfg.GossipAdvertiseAddr, Seeds: cfg.GossipSeeds,
		Fanout: cfg.Fanout, Interval: cfg.GossipInterval, AntiEntropyInterval: cfg.AntiEntropyInterval,
	}, auth, transport, gossip.NewSelector(cfg.DemoRandomSeed+time.Now().UnixNano()), members, store, registryService, eventRing, metrics, logger)
	registryService.SetMutationSink(engine.Enqueue)
	demoManager := demo.New(registryService, demo.Options{
		Enabled: cfg.DemoMode, Seed: cfg.DemoSeed, InstanceCount: cfg.DemoInstanceCount,
		RandomSeed: cfg.DemoRandomSeed, Logger: logger,
	})
	apiServer := api.New(api.Options{
		NodeID: cfg.NodeID, BootID: cfg.BootID, DemoMode: cfg.DemoMode, MaxBodyBytes: cfg.MaxHTTPBodyBytes,
		RateLimit: cfg.RateLimitPerMinute, AllowedOrigins: cfg.AllowedOrigins, Service: registryService,
		Members: members, Events: eventRing, Faults: faults, Metrics: metrics, Readiness: readiness,
		AntiEntropy: engine.AntiEntropyHandler(4 << 20), DemoOffline: demoManager.Offline, Logger: logger,
	})
	httpServer := &http.Server{
		Addr: cfg.HTTPAddr, Handler: apiServer.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	readiness.SetHTTP(true)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsChannel := make(chan error, 5)
	var workers sync.WaitGroup
	start := func(name string, function func() error) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if runErr := function(); runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, http.ErrServerClosed) {
				errorsChannel <- fmt.Errorf("%s: %w", name, runErr)
			}
		}()
	}
	start("HTTP server", func() error { return httpServer.Serve(listener) })
	start("Gossip engine", func() error { return engine.Run(ctx) })
	start("TTL wheel", func() error { return wheel.Run(ctx) })
	start("registry maintenance", func() error { return registryService.RunMaintenance(ctx) })
	start("demo manager", func() error { return demoManager.Run(ctx) })
	readiness.SetWorkers(true)
	start("readiness monitor", func() error {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				readiness.SetGossip(engine.Ready() && transport.LocalAddr() != nil)
			}
		}
	})
	logger.Info("Mini Eureka started", "node_id", cfg.NodeID, "boot_id", cfg.BootID, "http_addr", cfg.HTTPAddr, "gossip_addr", cfg.GossipAddr, "demo_mode", cfg.DemoMode)
	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errorsChannel:
		logger.Error("worker failed", "error", runErr)
	}
	readiness.SetHTTP(false)
	readiness.SetGossip(false)
	readiness.SetWorkers(false)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP shutdown incomplete", "error", err)
	}
	cancel()
	_ = engine.Close()
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		return fmt.Errorf("shutdown timed out: %w", shutdownCtx.Err())
	}
	logger.Info("Mini Eureka stopped", "node_id", cfg.NodeID)
	return runErr
}
