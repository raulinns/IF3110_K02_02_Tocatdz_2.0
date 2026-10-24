package web

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type healthResponse struct {
	Status string `json:"status"`
}

func NewRouter(logger *slog.Logger, routes ...security.RouteRegistrar) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealth)
	for _, route := range routes {
		route.RegisterRoutes(mux)
	}

	return middlewareStack(logger, mux)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(healthResponse{
		Status: "ok",
	})
}
