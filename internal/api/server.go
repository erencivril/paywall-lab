// Package api is the HTTP layer: routing, input validation and mapping
// service errors to status codes.
package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/erencivril/paywall-lab/internal/service"
	"github.com/erencivril/paywall-lab/internal/store"
	"github.com/erencivril/paywall-lab/web"
)

type Server struct {
	svc          *service.Service
	debugEnabled bool
}

// NewHandler wires the routes and middleware. Debug endpoints are only
// registered when debug is true.
func NewHandler(svc *service.Service, debug bool) http.Handler {
	s := &Server{svc: svc, debugEnabled: debug}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/paywall", s.paywall)
	mux.HandleFunc("POST /v1/purchases", s.purchase)
	mux.HandleFunc("POST /v1/restore", s.restore)
	mux.HandleFunc("GET /v1/entitlements/{user_id}", s.entitlements)
	mux.HandleFunc("POST /v1/store-notifications", s.notification)
	if debug {
		mux.HandleFunc("GET /v1/debug/events", s.debugEvents)
	}

	// The paywall page and its assets.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/paywall?country=US&user_id=demo-user", http.StatusFound)
	})
	mux.HandleFunc("GET /paywall", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web.FS, "paywall.html")
	})
	assets, _ := fs.Sub(web.FS, ".")
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))

	return recoverPanics(requestLog(mux))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.svc.Ping(ctx); err != nil {
		slog.Error("health check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) paywall(w http.ResponseWriter, r *http.Request) {
	country, err := validCountry(r.URL.Query().Get("country"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	userID := r.URL.Query().Get("user_id")
	if err := validID("user_id", userID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pw, err := s.svc.Paywall(r.Context(), country, userID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pw)
}

type purchaseRequest struct {
	UserID        string `json:"user_id"`
	ProductID     string `json:"product_id"`
	Store         string `json:"store"`
	Country       string `json:"country"`
	Sandbox       bool   `json:"sandbox"`
	StartTrial    bool   `json:"start_trial"`
	TransactionID string `json:"transaction_id"`
}

func (s *Server) purchase(w http.ResponseWriter, r *http.Request) {
	var req purchaseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	err := firstError(
		validID("user_id", req.UserID),
		validID("product_id", req.ProductID),
		validStore(req.Store),
		optionalID("transaction_id", req.TransactionID),
	)
	var country string
	if err == nil {
		country, err = validCountry(req.Country)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	res, err := s.svc.Purchase(r.Context(), service.PurchaseInput{
		UserID: req.UserID, ProductID: req.ProductID, Store: req.Store, Country: country,
		Sandbox: req.Sandbox, StartTrial: req.StartTrial, TransactionID: req.TransactionID,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	status := http.StatusCreated
	if res.Duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"duplicate": res.Duplicate, "subscription": res.Subscription})
}

type restoreRequest struct {
	UserID string `json:"user_id"`
	Store  string `json:"store"`
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	var req restoreRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := firstError(validID("user_id", req.UserID), validStore(req.Store)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	res, err := s.svc.Restore(r.Context(), service.RestoreInput{UserID: req.UserID, Store: req.Store})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"restored":       res.Restored,
		"restored_count": res.RestoredCount,
		"entitlement":    res.Entitlement,
	})
}

func (s *Server) entitlements(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if err := validID("user_id", userID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	view, err := s.svc.Entitlements(r.Context(), userID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) debugEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.EventFilter{UserID: q.Get("user_id"), EventType: q.Get("event_type")}
	events, err := s.svc.Events(r.Context(), filter)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrUnknownProduct):
		writeError(w, http.StatusUnprocessableEntity, "unknown_product", err.Error())
	case errors.Is(err, service.ErrAlreadySubscribed):
		writeError(w, http.StatusConflict, "already_subscribed", err.Error())
	case errors.Is(err, service.ErrTrialUsed):
		writeError(w, http.StatusConflict, "trial_already_used", err.Error())
	case errors.Is(err, service.ErrTransactionConflict):
		writeError(w, http.StatusConflict, "transaction_conflict", err.Error())
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, "unknown_transaction", err.Error())
	case errors.Is(err, service.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_transition", err.Error())
	case errors.Is(err, service.ErrMissingCreateDetails):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		s.serverError(w, r, err)
	}
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "error", err, "path", r.URL.Path, "request_id", w.Header().Get("X-Request-Id"))
	writeError(w, http.StatusInternalServerError, "internal_error", "something went wrong")
}

// firstError returns the first non-nil error.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func optionalID(field, value string) error {
	if value == "" {
		return nil
	}
	return validID(field, value)
}
