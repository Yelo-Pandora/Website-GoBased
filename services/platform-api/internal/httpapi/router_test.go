package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformauth "website-gobased/services/platform-api/internal/auth"
	"website-gobased/services/platform-api/internal/course"
)

type pingerStub struct {
	err error
}

type courseServiceStub struct{}

type courseServiceCaptureStub struct {
	listUserID *uint64
}

type authenticationServiceStub struct {
	loginResult platformauth.LoginResult
	loginErr    error
	session     platformauth.Session
	authErr     error
	csrfToken   string
	csrfValid   bool
	logoutErr   error
}

func (s authenticationServiceStub) Login(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) (platformauth.LoginResult, error) {
	return s.loginResult, s.loginErr
}

func (s authenticationServiceStub) Authenticate(
	_ context.Context,
	_ string,
) (platformauth.Session, error) {
	return s.session, s.authErr
}

func (s authenticationServiceStub) CSRFToken(_ platformauth.Session) string {
	return s.csrfToken
}

func (s authenticationServiceStub) ValidateCSRF(
	_ platformauth.Session,
	_ string,
) bool {
	return s.csrfValid
}

func (s authenticationServiceStub) Logout(_ context.Context, _ uint64) error {
	return s.logoutErr
}

func (courseServiceStub) List(
	_ context.Context,
	_ *uint64,
) ([]course.Course, error) {
	return []course.Course{{ID: 1, Slug: "standalone-architecture"}}, nil
}

func (courseServiceStub) Get(
	_ context.Context,
	slug string,
	_ *uint64,
) (course.Detail, error) {
	if slug == "missing" {
		return course.Detail{}, course.ErrNotFound
	}
	return course.Detail{Course: course.Course{ID: 1, Slug: slug}}, nil
}

func (s *courseServiceCaptureStub) List(
	_ context.Context,
	userID *uint64,
) ([]course.Course, error) {
	s.listUserID = userID
	return []course.Course{{ID: 1, Slug: "standalone-architecture"}}, nil
}

func (s *courseServiceCaptureStub) Get(
	_ context.Context,
	slug string,
	_ *uint64,
) (course.Detail, error) {
	return course.Detail{Course: course.Course{ID: 1, Slug: slug}}, nil
}

func (p pingerStub) PingContext(_ context.Context) error {
	return p.err
}

func TestReadiness(t *testing.T) {
	tests := []struct {
		name       string
		pingError  error
		wantStatus int
	}{
		{
			name:       "database available",
			wantStatus: http.StatusOK,
		},
		{
			name:       "database unavailable",
			pingError:  errors.New("database unavailable"),
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := NewRouter(
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				pingerStub{err: test.pingError},
				"http://lab-gateway:8080",
				courseServiceStub{},
				authenticationServiceStub{},
				AuthConfig{CookieName: "session"},
			)
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d; want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestSystemInfo(t *testing.T) {
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingerStub{},
		"http://lab-gateway:8080",
		courseServiceStub{},
		authenticationServiceStub{},
		AuthConfig{CookieName: "session"},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
	}
}

func TestCourseRoutes(t *testing.T) {
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingerStub{},
		"http://lab-gateway:8080",
		courseServiceStub{},
		authenticationServiceStub{},
		AuthConfig{CookieName: "session"},
	)

	tests := []struct {
		path       string
		wantStatus int
	}{
		{path: "/api/v1/courses", wantStatus: http.StatusOK},
		{path: "/api/v1/courses/standalone-architecture", wantStatus: http.StatusOK},
		{path: "/api/v1/courses/missing", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.wantStatus {
			t.Errorf("GET %s status = %d; want %d", test.path, response.Code, test.wantStatus)
		}
	}
}

func TestCourseListUsesAuthenticatedUser(t *testing.T) {
	courses := &courseServiceCaptureStub{}
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingerStub{},
		"http://lab-gateway:8080",
		courses,
		authenticationServiceStub{session: platformauth.Session{
			ID:   1,
			User: platformauth.User{ID: 7, Username: "learner", Status: "active"},
		}},
		AuthConfig{CookieName: "session"},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/courses", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "session-token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
	}
	if courses.listUserID == nil || *courses.listUserID != 7 {
		t.Fatalf("List() user ID = %v; want 7", courses.listUserID)
	}
	var body struct {
		Meta struct {
			Authenticated bool `json:"authenticated"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Meta.Authenticated {
		t.Fatal("meta.authenticated = false; want true")
	}
}

func TestCreateLabReturnsReservedResponse(t *testing.T) {
	session := platformauth.Session{ID: 1}
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingerStub{},
		"http://lab-gateway:8080",
		courseServiceStub{},
		authenticationServiceStub{session: session, csrfValid: true},
		AuthConfig{CookieName: "session"},
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/labs", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "test-token"})
	request.Header.Set(csrfHeaderName, "test-csrf")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotImplemented {
		t.Fatalf(
			"status = %d; want %d",
			response.Code,
			http.StatusNotImplemented,
		)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "LABS_NOT_IMPLEMENTED" {
		t.Errorf(
			"error code = %q; want LABS_NOT_IMPLEMENTED",
			body.Error.Code,
		)
	}
}

func TestAuthenticationRoutes(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	session := platformauth.Session{
		ID: 1,
		User: platformauth.User{
			ID:       7,
			Username: "learner",
			Status:   "active",
		},
		Token:     "session-token",
		ExpiresAt: expiresAt,
	}
	authentication := authenticationServiceStub{
		loginResult: platformauth.LoginResult{
			Session:   session,
			CSRFToken: "csrf-token",
		},
		session:   session,
		csrfToken: "csrf-token",
		csrfValid: true,
	}
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingerStub{},
		"http://lab-gateway:8080",
		courseServiceStub{},
		authentication,
		AuthConfig{CookieName: "session"},
	)

	t.Run("login", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/auth/login",
			strings.NewReader(`{"username":"learner","password":"password"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != "session" {
			t.Fatalf("cookies = %v; want Session Cookie", cookies)
		}
		if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("Session Cookie flags = %#v", cookies[0])
		}
	})

	t.Run("current user requires authentication", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d; want %d", response.Code, http.StatusUnauthorized)
		}
	})

	t.Run("current user", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		request.AddCookie(&http.Cookie{Name: "session", Value: "session-token"})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
		}
	})

	t.Run("logout", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		request.AddCookie(&http.Cookie{Name: "session", Value: "session-token"})
		request.Header.Set(csrfHeaderName, "csrf-token")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
		}
	})
}

func TestLogoutRejectsInvalidCSRF(t *testing.T) {
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingerStub{},
		"http://lab-gateway:8080",
		courseServiceStub{},
		authenticationServiceStub{
			session:   platformauth.Session{ID: 1},
			csrfValid: false,
		},
		AuthConfig{CookieName: "session"},
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "session-token"})
	request.Header.Set(csrfHeaderName, "wrong-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusForbidden)
	}
}
