// Package api exposes the control plane over a small REST surface.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/idgen"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/reputation"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/scheduler"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/simulator"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/throttle"
	"github.com/gorilla/mux"
)

// Server bundles the HTTP handlers and their dependencies.
type Server struct {
	Queue *scheduler.Queue
	Sched *scheduler.Scheduler
	Hier  *throttle.Hierarchy
	Rep   *reputation.Store
	Fleet *simulator.Fleet
}

// Router builds the HTTP router.
func (s *Server) Router() http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/healthz", s.health).Methods(http.MethodGet)
	r.HandleFunc("/v1/messages", s.enqueue).Methods(http.MethodPost)
	r.HandleFunc("/v1/stats", s.statsHandler).Methods(http.MethodGet)
	r.HandleFunc("/v1/rates", s.rates).Methods(http.MethodGet)
	r.HandleFunc("/v1/reputation", s.reputation).Methods(http.MethodGet)
	r.HandleFunc("/v1/breakers", s.breakers).Methods(http.MethodGet)
	r.HandleFunc("/v1/attempts", s.attempts).Methods(http.MethodGet)
	r.HandleFunc("/v1/simulate/{provider}/{action}", s.simulate).Methods(http.MethodPost)
	return r
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type enqueueReq struct {
	CustomerID string `json:"customer_id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Domain     string `json:"domain"`
	Provider   string `json:"provider"`
	Subject    string `json:"subject"`
	Priority   int    `json:"priority"`
	Count      int    `json:"count"` // bulk enqueue helper
}

func (s *Server) enqueue(w http.ResponseWriter, r *http.Request) {
	var req enqueueReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	n := req.Count
	if n < 1 {
		n = 1
	}
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		m := model.Message{
			ID:         idgen.New(),
			CustomerID: req.CustomerID,
			From:       req.From,
			To:         req.To,
			Domain:     req.Domain,
			Provider:   req.Provider,
			Subject:    req.Subject,
			Priority:   req.Priority,
			EnqueuedAt: time.Now(),
		}
		s.Queue.Enqueue(m)
		ids = append(ids, m.ID)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"enqueued": n, "ids": ids})
}

func (s *Server) statsHandler(w http.ResponseWriter, _ *http.Request) {
	st := s.Sched.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"queue_depth": s.Queue.Depth(),
		"sent":        st.Sent,
		"deferred":    st.Deferred,
		"bounced":     st.Bounced,
		"timeouts":    st.Timeouts,
		"throttled":   st.Throttled,
	})
}

func (s *Server) rates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Hier.Snapshot())
}

func (s *Server) reputation(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Rep.Snapshot())
}

func (s *Server) breakers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Sched.BreakerStates())
}

func (s *Server) attempts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Sched.RecentAttempts())
}

func (s *Server) simulate(w http.ResponseWriter, r *http.Request) {
	v := mux.Vars(r)
	provider, action := v["provider"], v["action"]
	switch action {
	case "throttle":
		s.Fleet.SetThrottle(provider, true)
	case "recover":
		s.Fleet.SetThrottle(provider, false)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"provider": provider, "action": action})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
