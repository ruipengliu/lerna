// Package host implements local configuration, dependency probes and bounded shutdown.
// Domain recovery, migrations, transports and worker handlers are separate future slices.
package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

type hostStatus struct {
	Protocol       string          `json:"protocol"`
	Role           string          `json:"role"`
	InstanceID     string          `json:"instance_id"`
	BootID         string          `json:"boot_id"`
	Phase          string          `json:"phase"`
	AcceptingTasks bool            `json:"accepting_tasks"`
	Dependencies   map[string]bool `json:"dependencies"`
	Missing        []string        `json:"missing"`
}

type serverState struct {
	config   Config
	bootID   string
	ping     func(context.Context) error
	draining atomic.Bool
}

func (state *serverState) status(ctx context.Context) hostStatus {
	timeout, _ := time.ParseDuration(state.config.ProbeTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dependencies := map[string]bool{"database": state.ping(ctx) == nil}
	for name, path := range map[string]string{"content_root": state.config.ContentRoot, "managed_root": state.config.ManagedRoot} {
		info, err := os.Stat(path)
		dependencies[name] = err == nil && info.IsDir()
	}
	missing := []string{"domain_implementation", "recovery_and_release_checks"}
	phase := "skeleton"
	if state.draining.Load() {
		phase = "draining"
	}
	return hostStatus{"lerna-dev-status/1", state.config.Role, state.config.InstanceID, state.bootID, phase, false, dependencies, missing}
}

func dependenciesAvailable(status hostStatus) bool {
	for _, available := range status.Dependencies {
		if !available {
			return false
		}
	}
	return status.Phase != "draining"
}

func (state *serverState) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"alive": true})
	})
	mux.HandleFunc("GET /health/startup", func(w http.ResponseWriter, r *http.Request) {
		status := state.status(r.Context())
		code := http.StatusOK
		if !dependenciesAvailable(status) {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, status)
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, state.status(r.Context()))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, state.status(r.Context()))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not_implemented", "message": "Only local host diagnostics are implemented in this skeleton."})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func Run(ctx context.Context, config Config, logger *slog.Logger) error {
	if err := config.validate(); err != nil {
		return err
	}
	if logger == nil {
		logger = slog.Default()
	}
	probeTimeout, _ := time.ParseDuration(config.ProbeTimeout)
	probeContext, cancel := context.WithTimeout(ctx, probeTimeout)
	database, err := openDatabase(probeContext, config.Database)
	cancel()
	if err != nil {
		return err
	}
	defer database.close()
	bootBytes := make([]byte, 16)
	if _, err := rand.Read(bootBytes); err != nil {
		return err
	}
	state := &serverState{config: config, bootID: "boot_" + hex.EncodeToString(bootBytes), ping: database.ping}
	if !dependenciesAvailable(state.status(ctx)) {
		return errors.New("configured data roots are missing; initialize or restore the original roots explicitly")
	}
	listener, err := net.Listen("tcp", config.AdminAddr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: state.handler(), ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	logger.Info("local host started", "role", config.Role, "instance_id", config.InstanceID, "boot_id", state.bootID, "admin_addr", config.AdminAddr, "phase", "skeleton", "accepting_tasks", false)
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		state.draining.Store(true)
		logger.Info("local host draining", "role", config.Role, "boot_id", state.bootID)
		timeout, _ := time.ParseDuration(config.ShutdownTimeout)
		shutdownContext, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
			return err
		}
		if err := <-finished; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		logger.Info("local host stopped", "role", config.Role, "boot_id", state.bootID)
		return nil
	}
}
