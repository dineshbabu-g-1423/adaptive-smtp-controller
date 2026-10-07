// Command controller runs the Adaptive SMTP Traffic Controller control plane:
// a REST API + a background delivery scheduler driving simulated providers.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/app"
)

func main() {
	var (
		addr       = flag.String("addr", envOr("ADDR", ":8080"), "HTTP listen address")
		limitsPath = flag.String("limits", "configs/limits.yaml", "limits config path")
		poolsPath  = flag.String("pools", "configs/providers.yaml", "IP pool config path")
		warmupPath = flag.String("warmup", "configs/warmup.yaml", "warmup config path")
		tick       = flag.Duration("tick", 10*time.Millisecond, "scheduler tick interval")
	)
	flag.Parse()

	comps, err := app.Build(*limitsPath, *poolsPath, *warmupPath)
	if err != nil {
		log.Fatalf("build: %v", err)
	}

	go comps.Sched.Run(*tick)

	srv := &http.Server{
		Addr:         *addr,
		Handler:      comps.API.Router(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("adaptive-smtp-controller listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("shutting down...")
	comps.Sched.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
