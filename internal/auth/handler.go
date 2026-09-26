package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"observastack/internal/middleware"
	"observastack/internal/observability"
)

// errBodyTooLarge signals that the request body exceeded the configured limit.
var errBodyTooLarge = errors.New("request body too large")

// decodeJSONBody decodes the request body into dst.
//
// An oversized body is reported as errBodyTooLarge rather than a generic decode
// failure, so chunked requests get the same 413 that middleware.BodyLimit
// returns on the Content-Length fast path.
func decodeJSONBody(r *http.Request, dst any) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		if middleware.IsBodyTooLarge(err) {
			return errBodyTooLarge
		}
		return err
	}
	return nil
}

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

// writeDecodeError logs a body-decode failure server-side and writes the
// matching client response. The raw error is never sent to the client.
func (h *Handler) writeDecodeError(w http.ResponseWriter, r *http.Request, start time.Time, err error) {
	if errors.Is(err, errBodyTooLarge) {
		h.logger.Error(r.Context(), r.Method, r.URL.Path, http.StatusRequestEntityTooLarge,
			time.Since(start), "request body too large", err)
		writeError(w, http.StatusRequestEntityTooLarge, middleware.CodePayloadTooLarge,
			"Request body exceeds maximum allowed size")
		return
	}

	// Deliberately logs neither the password nor the raw email: a decode
	// failure can contain attacker-controlled content, and LogMiddleware
	// already records trace_id/method/path/status for this request.
	h.logger.Error(r.Context(), r.Method, r.URL.Path, http.StatusUnprocessableEntity,
		time.Since(start), "decode request body", err)
	writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid request body")
}

// registerResponse mirrors the success envelope in docs/API_SPEC.md.
// tenantId is returned so the client can send it as X-Tenant-ID.
type registerResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
	TenantID  string    `json:"tenantId"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		h.writeDecodeError(w, r, start, err)
		return
	}

	registration, err := h.service.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		// errors.Is, not ==: the service wraps repository errors, and a lost
		// INSERT race arrives here as a wrapped ErrEmailAlreadyExists.
		switch {
		case errors.Is(err, ErrEmailAlreadyExists):
			writeError(w, http.StatusConflict, "EMAIL_EXISTS", "Email already registered")
		case errors.Is(err, ErrTenantAlreadyExists):
			writeError(w, http.StatusConflict, "TENANT_EXISTS", "Tenant already exists")
		case errors.Is(err, ErrInvalidEmail):
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid email format")
		case errors.Is(err, ErrPasswordTooShort):
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Password must be at least 8 characters")
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Registration failed")
		}
		return
	}

	h.logger.Info(r.Context(), "INFO", r.Method, r.URL.Path, http.StatusCreated,
		time.Since(start), "registration accepted for "+observability.MaskEmail(registration.User.Email))

	writeJSON(w, http.StatusCreated, map[string]any{"data": registerResponse{
		ID:        registration.User.ID,
		Email:     registration.User.Email,
		CreatedAt: registration.User.CreatedAt,
		TenantID:  registration.TenantID,
	}})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		h.writeDecodeError(w, r, start, err)
		return
	}

	tokens, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid email or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Login failed")
		return
	}

	// Success only: logging failures here would let an attacker flood the log
	// with rejected attempts. LogMiddleware already records the 401.
	h.logger.Info(r.Context(), "INFO", r.Method, r.URL.Path, http.StatusOK,
		time.Since(start), "login accepted for "+observability.MaskEmail(req.Email))

	writeJSON(w, http.StatusOK, map[string]any{"data": tokens})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		h.writeDecodeError(w, r, start, err)
		return
	}

	tokens, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrRefreshTokenInvalid) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired refresh token")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Refresh failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": tokens})
}

// Logout revokes a refresh token. It is idempotent by design, so a valid or
// unknown token both yield 200 and the endpoint reveals nothing about which
// tokens exist.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		h.writeDecodeError(w, r, start, err)
		return
	}

	if err := h.service.Logout(r.Context(), req.RefreshToken); err != nil {
		if errors.Is(err, ErrRefreshTokenInvalid) {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Missing refresh token")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Logout failed")
		return
	}

	h.logger.Info(r.Context(), "INFO", r.Method, r.URL.Path, http.StatusOK,
		time.Since(start), "logout completed")

	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "logged out"}})
}
