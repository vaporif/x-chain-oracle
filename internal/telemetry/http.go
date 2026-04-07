package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var startedAt = time.Now()

func (t *Telemetry) ServeHTTP(ctx context.Context) (string, error) {
	mux := http.NewServeMux()

	if t.promExporter != nil {
		mux.Handle("GET /metrics", promhttp.Handler())
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"uptime_seconds": int(time.Since(startedAt).Seconds()),
		})
	})

	addr := fmt.Sprintf(":%d", t.Config.HTTPPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	go server.Serve(ln)

	return ln.Addr().String(), nil
}
