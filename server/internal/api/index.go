package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

func handleIndexStatus(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.Index == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "index_unavailable", "The index is not available.")
			return
		}
		status, err := opts.Index.ScanStatus(r.Context())
		if err != nil {
			opts.Logger.Error("read index status", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "index_status_failed", "Could not read index status.")
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func handleIndexRescan(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.Scanner == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "index_unavailable", "The index is not available.")
			return
		}

		var req struct {
			Full bool `json:"full"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", "Request body must be JSON like {\"full\": true}.")
			return
		}

		if opts.Scanner.Running() {
			writeProblem(w, r, http.StatusConflict, "scan_running", "A scan is already in progress.")
			return
		}

		// Scans outlive the request; a full scan of a large vault is minutes,
		// not milliseconds.
		go func(full bool) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			stats, err := opts.Scanner.Run(ctx, full)
			if err != nil {
				opts.Logger.Error("index rescan failed", "error", err, "full", full)
				return
			}
			opts.Logger.Info("index rescan complete",
				"files", stats.Total, "indexed", stats.Indexed, "updated", stats.Updated, "removed", stats.Removed)
		}(req.Full)

		writeJSON(w, http.StatusAccepted, map[string]any{"state": "scanning", "full": req.Full})
	}
}
