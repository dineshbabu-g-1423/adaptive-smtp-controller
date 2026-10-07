// Command simulator runs a self-contained, offline demonstration of the
// controller reacting to a provider incident ("the killer demo"):
//
//	go run ./cmd/simulator outlook-throttle
//	go run ./cmd/simulator outlook-recovery
//	go run ./cmd/simulator steady
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/app"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/idgen"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

func main() {
	scenario := "outlook-throttle"
	if len(os.Args) > 1 {
		scenario = os.Args[1]
	}

	comps, err := app.Build("configs/limits.yaml", "configs/providers.yaml", "configs/warmup.yaml")
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	go comps.Sched.Run(5 * time.Millisecond)

	providers := []string{"gmail", "outlook", "yahoo"}
	feed := func(d time.Duration) {
		stop := time.After(d)
		t := time.NewTicker(2 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				for _, p := range providers {
					comps.Queue.Enqueue(model.Message{
						ID: idgen.New(), CustomerID: "acme", From: "news@acme.io",
						To: "user@" + p + ".com", Domain: "acme.io", Provider: p,
						EnqueuedAt: time.Now(), NextAttempt: time.Now(),
					})
				}
			}
		}
	}

	switch scenario {
	case "outlook-throttle":
		runIncident(comps, feed)
	case "outlook-recovery":
		runRecovery(comps, feed)
	case "steady":
		fmt.Println("== STEADY STATE (5s) ==")
		go feed(5 * time.Second)
		time.Sleep(5 * time.Second)
		printDashboard(comps)
	default:
		fmt.Println("unknown scenario:", scenario)
		fmt.Println("try: outlook-throttle | outlook-recovery | steady")
	}
}

func runIncident(comps *app.Components, feed func(time.Duration)) {
	fmt.Println("== BASELINE ==")
	go feed(3 * time.Second)
	time.Sleep(3 * time.Second)
	printDashboard(comps)

	fmt.Println("\n>>> $ deliveryctl simulate outlook-throttle")
	comps.Fleet.SetThrottle("outlook", true)
	go feed(4 * time.Second)
	for i := 0; i < 4; i++ {
		time.Sleep(1 * time.Second)
	}
	fmt.Println("\n== OUTLOOK THROTTLING DETECTED ==")
	printDashboard(comps)
	fmt.Println("\nBlast radius: OUTLOOK only — Gmail & Yahoo unaffected.")
}

func runRecovery(comps *app.Components, feed func(time.Duration)) {
	comps.Fleet.SetThrottle("outlook", true)
	go feed(3 * time.Second)
	time.Sleep(3 * time.Second)
	fmt.Println("== DURING INCIDENT ==")
	printDashboard(comps)

	fmt.Println("\n>>> $ deliveryctl simulate outlook-recovery")
	comps.Fleet.SetThrottle("outlook", false)
	go feed(6 * time.Second)
	time.Sleep(6 * time.Second)
	fmt.Println("\n== RECOVERY DETECTED ==")
	printDashboard(comps)
}

func printDashboard(comps *app.Components) {
	st := comps.Sched.Stats()
	fmt.Printf("  queue_depth=%d sent=%d deferred=%d bounced=%d timeouts=%d throttled=%d\n",
		comps.Queue.Depth(), st.Sent, st.Deferred, st.Bounced, st.Timeouts, st.Throttled)

	fmt.Println("  provider rates (current/base):")
	for name, v := range comps.Hier.Snapshot() {
		if len(name) >= 9 && name[:9] == "provider:" {
			fmt.Printf("    %-18s %s msg/s\n", name, v)
		}
	}
	fmt.Println("  circuit states:")
	for p, s := range comps.Sched.BreakerStates() {
		fmt.Printf("    %-10s %s\n", p, s)
	}
}
