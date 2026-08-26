package service

import (
	"context"
	"time"

	"minieureka/internal/ttl"
)

func (s *Service) RunMaintenance(ctx context.Context) error {
	interval := min(s.opts.EvictedDisplayTTL, time.Minute)
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			s.HandleTasks([]ttl.Task{{Kind: ttl.Collect, Deadline: now}})
		}
	}
}
