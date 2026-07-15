package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/httpserver"
	platformauth "website-gobased/services/platform-api/internal/auth"
)

const (
	csrfHeaderName  = "X-CSRF-Token"
	maxLoginBodyLen = 4096
	authSessionKey  = "authSession"
)

// AuthConfig controls Session Cookie behavior at the HTTP boundary.
type AuthConfig struct {
	CookieName   string
	CookieSecure bool
}

type authenticationService interface {
	Login(
		ctx context.Context,
		username string,
		password string,
		clientKey string,
	) (platformauth.LoginResult, error)
	Authenticate(ctx context.Context, token string) (platformauth.Session, error)
	CSRFToken(session platformauth.Session) string
	ValidateCSRF(session platformauth.Session, token string) bool
	Logout(ctx context.Context, sessionID uint64) error
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *handler) login(ctx *gin.Context) {
	var request loginRequest
	if err := decodeJSON(ctx, &request); err != nil {
		httpserver.WriteError(
			ctx,
			http.StatusBadRequest,
			"VALIDATION_FAILED",
			"invalid login request",
		)
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" || len(request.Username) > 64 ||
		request.Password == "" || len(request.Password) > 72 {
		httpserver.WriteError(
			ctx,
			http.StatusBadRequest,
			"VALIDATION_FAILED",
			"invalid login request",
		)
		return
	}

	result, err := h.authentication.Login(
		ctx.Request.Context(),
		request.Username,
		request.Password,
		loginClientKey(ctx),
	)
	if errors.Is(err, platformauth.ErrInvalidCredentials) {
		httpserver.WriteError(
			ctx,
			http.StatusUnauthorized,
			"INVALID_CREDENTIALS",
			"invalid username or password",
		)
		return
	}
	if errors.Is(err, platformauth.ErrAccountDisabled) {
		httpserver.WriteError(
			ctx,
			http.StatusForbidden,
			"ACCOUNT_DISABLED",
			"account is disabled",
		)
		return
	}
	if errors.Is(err, platformauth.ErrRateLimited) {
		httpserver.WriteError(
			ctx,
			http.StatusTooManyRequests,
			"LOGIN_RATE_LIMITED",
			"too many login attempts",
		)
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "login", "error", err)
		httpserver.WriteError(
			ctx,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"unable to log in",
		)
		return
	}

	h.setSessionCookie(ctx, result.Session.Token, result.Session.ExpiresAt)
	ctx.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"user":             result.Session.User,
			"csrfToken":        result.CSRFToken,
			"sessionExpiresAt": result.Session.ExpiresAt,
		},
	})
}

func (h *handler) currentUser(ctx *gin.Context) {
	session, ok := currentSession(ctx)
	if !ok {
		httpserver.WriteError(
			ctx,
			http.StatusUnauthorized,
			"AUTH_REQUIRED",
			"authentication is required",
		)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"user":             session.User,
			"csrfToken":        h.authentication.CSRFToken(session),
			"sessionExpiresAt": session.ExpiresAt,
		},
	})
}

func (h *handler) logout(ctx *gin.Context) {
	session, ok := currentSession(ctx)
	if !ok {
		httpserver.WriteError(
			ctx,
			http.StatusUnauthorized,
			"AUTH_REQUIRED",
			"authentication is required",
		)
		return
	}
	if err := h.authentication.Logout(ctx.Request.Context(), session.ID); err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "logout", "error", err)
		httpserver.WriteError(
			ctx,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"unable to log out",
		)
		return
	}
	h.clearSessionCookie(ctx)
	ctx.JSON(http.StatusOK, gin.H{"data": gin.H{"loggedOut": true}})
}

func (h *handler) optionalAuthentication(ctx *gin.Context) {
	token, err := ctx.Cookie(h.authConfig.CookieName)
	if errors.Is(err, http.ErrNoCookie) {
		ctx.Next()
		return
	}
	if err != nil {
		h.clearSessionCookie(ctx)
		ctx.Next()
		return
	}

	session, err := h.authentication.Authenticate(ctx.Request.Context(), token)
	if errors.Is(err, platformauth.ErrInvalidSession) {
		h.clearSessionCookie(ctx)
		ctx.Next()
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "authenticate request", "error", err)
		httpserver.WriteError(
			ctx,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"unable to authenticate request",
		)
		return
	}
	ctx.Set(authSessionKey, session)
	ctx.Next()
}

func (h *handler) requireAuthentication(ctx *gin.Context) {
	if _, ok := currentSession(ctx); !ok {
		httpserver.WriteError(
			ctx,
			http.StatusUnauthorized,
			"AUTH_REQUIRED",
			"authentication is required",
		)
		return
	}
	ctx.Next()
}

func (h *handler) requireCSRF(ctx *gin.Context) {
	session, ok := currentSession(ctx)
	if !ok {
		httpserver.WriteError(
			ctx,
			http.StatusUnauthorized,
			"AUTH_REQUIRED",
			"authentication is required",
		)
		return
	}
	if !h.authentication.ValidateCSRF(session, ctx.GetHeader(csrfHeaderName)) {
		httpserver.WriteError(
			ctx,
			http.StatusForbidden,
			"CSRF_INVALID",
			"csrf token is invalid",
		)
		return
	}
	ctx.Next()
}

func currentSession(ctx *gin.Context) (platformauth.Session, bool) {
	value, ok := ctx.Get(authSessionKey)
	if !ok {
		return platformauth.Session{}, false
	}
	session, ok := value.(platformauth.Session)
	return session, ok
}

func (h *handler) setSessionCookie(
	ctx *gin.Context,
	token string,
	expiresAt time.Time,
) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     h.authConfig.CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.authConfig.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *handler) clearSessionCookie(ctx *gin.Context) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     h.authConfig.CookieName,
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.authConfig.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func decodeJSON(ctx *gin.Context, destination any) error {
	ctx.Request.Body = http.MaxBytesReader(
		ctx.Writer,
		ctx.Request.Body,
		maxLoginBodyLen,
	)
	decoder := json.NewDecoder(ctx.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func loginClientKey(ctx *gin.Context) string {
	forwardedFor := strings.TrimSpace(ctx.GetHeader("X-Forwarded-For"))
	if forwardedFor != "" {
		address, _, _ := strings.Cut(forwardedFor, ",")
		return strings.TrimSpace(address)
	}
	host, _, err := net.SplitHostPort(ctx.Request.RemoteAddr)
	if err == nil {
		return host
	}
	return ctx.Request.RemoteAddr
}
