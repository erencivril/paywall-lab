package api

import (
	"net/http"
	"time"

	"github.com/erencivril/paywall-lab/internal/service"
)

type notificationRequest struct {
	NotificationID        string  `json:"notification_id"`
	Type                  string  `json:"type"`
	OriginalTransactionID string  `json:"original_transaction_id"`
	ExpiresAt             *string `json:"expires_at"`

	// Only read for SUBSCRIBED on a transaction that is not in the database.
	UserID     string `json:"user_id"`
	ProductID  string `json:"product_id"`
	Store      string `json:"store"`
	Country    string `json:"country"`
	Sandbox    bool   `json:"sandbox"`
	StartTrial bool   `json:"start_trial"`
}

func (s *Server) notification(w http.ResponseWriter, r *http.Request) {
	var req notificationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := req.toInput()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	res, err := s.svc.HandleNotification(r.Context(), in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (req notificationRequest) toInput() (service.NotificationInput, error) {
	in := service.NotificationInput{
		NotificationID: req.NotificationID, Type: req.Type, OriginalTransactionID: req.OriginalTransactionID,
		UserID: req.UserID, ProductID: req.ProductID, Store: req.Store, Sandbox: req.Sandbox, StartTrial: req.StartTrial,
	}
	if err := firstError(
		validID("notification_id", req.NotificationID),
		validID("original_transaction_id", req.OriginalTransactionID),
		validType(req.Type),
		optionalID("user_id", req.UserID),
		optionalID("product_id", req.ProductID),
	); err != nil {
		return in, err
	}
	if req.Store != "" {
		if err := validStore(req.Store); err != nil {
			return in, err
		}
	}
	if req.Country != "" {
		country, err := validCountry(req.Country)
		if err != nil {
			return in, err
		}
		in.Country = country
	}
	if req.ExpiresAt != nil {
		if req.Type != service.NotifyDidRenew {
			return in, invalid("expires_at is only accepted for DID_RENEW")
		}
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return in, invalid("expires_at must be an RFC 3339 timestamp")
		}
		in.ExpiresAt = &t
	}
	return in, nil
}

func validType(value string) error {
	switch value {
	case service.NotifySubscribed, service.NotifyDidRenew, service.NotifyExpired, service.NotifyRefund:
		return nil
	case "":
		return invalid("type is required")
	}
	return invalid("type must be one of SUBSCRIBED, DID_RENEW, EXPIRED, REFUND")
}
