package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/game-asset-optimizer/internal/gameassets"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	bucket := os.Getenv("ASSET_BUCKET")
	if bucket == "" {
		bucket = "game-optimized-assets"
	}
	pipeline := gameassets.Pipeline{Client: gameassets.NewClient(apiKey), Bucket: bucket}
	if err := pipeline.Initialize(context.Background()); err != nil {
		log.Fatalf("initialize asset bucket: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /assets/optimize", optimizeHandler(pipeline))
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("asset service listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func optimizeHandler(pipeline gameassets.Pipeline) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var input gameassets.AssetRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		result, err := pipeline.Process(r.Context(), input)
		if err != nil {
			var apiErr *gameassets.InfraiError
			if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
				writeJSON(w, apiErr.Status, map[string]string{"error": apiErr.Error(), "code": apiErr.Code})
				return
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "asset processing failed"})
			return
		}
		status := http.StatusAccepted
		if result.Decision.State == "stored" {
			status = http.StatusCreated
		}
		writeJSON(w, status, result)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
