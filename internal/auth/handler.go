package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"observastack/internal/observability"
)

type Handler struct {
	service *Service
	logger  *observability.Logger
}

// NewHandler builds the auth HTTP handlers.
//
// A logger is required rather than optional so handlers cannot silently fall
// back to unstructured logging. Passwords must never reach the log output in
// any form, not even masked.
func NewHandler(service *Service, logger *observability.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// maskEmail redacts the local part of an email so a log entry can identify its
// subject without recording the full address.
func maskEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Deliberately logs neither the password nor the raw email: a decode
		// failure can contain attacker-controlled content, and LogMiddleware
		// already records trace_id/method/path/status for this request.
		h.logger.Error(r.Context(), r.Method, r.URL.Path, http.StatusUnprocessableEntity,
			time.Since(start), "decode request body", err)
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid request body")
		return
	}

	user, err := h.service.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		switch err {
		case ErrEmailAlreadyExists:
			writeError(w, http.StatusConflict, "EMAIL_EXISTS", "Email already registered")
		case ErrInvalidEmail:
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid email format")
		case ErrPasswordTooShort:
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Password must be at least 8 characters")
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Registration failed")
		}
		return
	}

	h.logger.Info(r.Context(), "INFO", r.Method, r.URL.Path, http.StatusCreated,
		time.Since(start), "registration accepted for "+maskEmail(user.Email))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"data": user})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid request body")
		return
	}

	tokens, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if err == ErrInvalidCredentials {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid email or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Login failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"data": tokens})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid request body")
		return
	}

	tokens, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		if err == ErrRefreshTokenInvalid {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired refresh token")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Refresh failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"data": tokens})
}
