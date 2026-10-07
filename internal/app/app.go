// Package app wires every component together from configuration.
package app

import (
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/api"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/circuitbreaker"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/config"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/reputation"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/retry"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/routing"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/scheduler"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/simulator"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/throttle"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/warmup"
)

// Components is the fully-wired control plane.
type Components struct {
	Queue  *scheduler.Queue
	Hier   *throttle.Hierarchy
	Rep    *reputation.Store
	Router *routing.Router
	Warm   *warmup.Engine
	Fleet  *simulator.Fleet
	Sched  *scheduler.Scheduler
	API    *api.Server
}

// Build constructs all components from the three config files.
func Build(limitsPath, poolsPath, warmupPath string) (*Components, error) {
	var limits config.Limits
	if err := config.Load(limitsPath, &limits); err != nil {
		return nil, err
	}
	var pools config.Pools
	if err := config.Load(poolsPath, &pools); err != nil {
		return nil, err
	}
	var wcfg config.Warmup
	if err := config.Load(warmupPath, &wcfg); err != nil {
		return nil, err
	}

	hier := throttle.NewHierarchy(limits.Throttle)
	rep := reputation.NewStore()
	for _, seed := range pools.Reputation {
		rep.Seed(seed.IP, seed.Provider, seed.Score)
	}

	warm := warmup.NewEngine(wcfg.Thresholds)
	for _, s := range wcfg.Schedules {
		warm.Register(s)
	}

	router := routing.New(pools.IPs, rep, warm)
	fleet := simulator.NewFleet()
	queue := scheduler.NewQueue()

	breakers := map[string]*circuitbreaker.Breaker{}
	sched := scheduler.New(scheduler.Deps{
		Queue:     queue,
		Hier:      hier,
		Router:    router,
		Rep:       rep,
		Transport: fleet,
		Policy:    retry.DefaultPolicy(),
		Breakers:  breakers,
	})

	srv := &api.Server{Queue: queue, Sched: sched, Hier: hier, Rep: rep, Fleet: fleet}

	return &Components{
		Queue: queue, Hier: hier, Rep: rep, Router: router, Warm: warm,
		Fleet: fleet, Sched: sched, API: srv,
	}, nil
}
