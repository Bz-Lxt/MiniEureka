package observe

import (
	"sync/atomic"
)

type Readiness struct {
	http    atomic.Bool
	gossip  atomic.Bool
	workers atomic.Bool
}

type Checks struct {
	HTTP    bool `json:"http"`
	Gossip  bool `json:"gossip"`
	Workers bool `json:"workers"`
}

func (r *Readiness) SetHTTP(value bool)    { r.http.Store(value) }
func (r *Readiness) SetGossip(value bool)  { r.gossip.Store(value) }
func (r *Readiness) SetWorkers(value bool) { r.workers.Store(value) }

func (r *Readiness) Checks() Checks {
	return Checks{HTTP: r.http.Load(), Gossip: r.gossip.Load(), Workers: r.workers.Load()}
}

func (r *Readiness) Ready() bool {
	checks := r.Checks()
	return checks.HTTP && checks.Gossip && checks.Workers
}
