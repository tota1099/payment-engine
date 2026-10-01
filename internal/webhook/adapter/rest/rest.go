// Package rest receives PSP webhooks: read, authenticate (signature + replay
// window, by the PSP adapter) and hand the event to the Ingest use case.
package rest

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/renanporto/payment-engine/internal/httpx"
	"github.com/renanporto/payment-engine/internal/kernel"
	"github.com/renanporto/payment-engine/internal/psp"
	"github.com/renanporto/payment-engine/internal/webhook/app"
)

// Verifier is the part of a PSP adapter that authenticates its webhooks.
type Verifier interface {
	Name() string
	ParseWebhook(header http.Header, body []byte) (psp.Event, error)
}

type Handler struct {
	PSP    Verifier
	Ingest app.Ingester
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/webhooks", httpx.Handle(h.receive))
}

func (h *Handler) receive(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return kernel.Invalid("unreadable body")
	}
	ev, err := h.PSP.ParseWebhook(r.Header, body)
	if err != nil {
		slog.WarnContext(ctx, "webhook rejected", "provider", h.PSP.Name(), "err", err)
		return kernel.Invalid("invalid webhook")
	}
	res, err := h.Ingest.Ingest(ctx, h.PSP.Name(), ev)
	if err != nil {
		return err
	}
	log := slog.With("provider", h.PSP.Name(), "event_id", ev.ID, "type", ev.Type, "external_id", ev.ExternalID)
	if res.Ignored != "" {
		log.WarnContext(ctx, "webhook ignored", "reason", res.Ignored)
	} else {
		log.InfoContext(ctx, "webhook processed", "duplicate", res.Duplicate)
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"received": true})
	return nil
}
