// Package auth imitates the existing corporate SSO: it issues a short-lived
// signed session (JWT in an HttpOnly cookie) for synthetic directory users.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"tropa/internal/directory"
)

// CookieName is the session cookie.
const CookieName = "tropa_session"

// ErrBadCredentials is returned on wrong login/password.
var ErrBadCredentials = errors.New("неверный логин или пароль")

// Auth issues and verifies sessions.
type Auth struct {
	secret       []byte
	dir          *directory.Directory
	ttl          time.Duration
	secureCookie bool
}

// New creates an authenticator.
func New(secret string, dir *directory.Directory, ttl time.Duration, secureCookie bool) *Auth {
	return &Auth{secret: []byte(secret), dir: dir, ttl: ttl, secureCookie: secureCookie}
}

// Login checks credentials and returns a signed token.
func (a *Auth) Login(login, password string) (*directory.Person, string, error) {
	p, ok := a.dir.ByLogin(strings.ToLower(strings.TrimSpace(login)))
	if !ok {
		// constant-ish time: still run bcrypt to avoid user enumeration by timing
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$Dv/RbCZ1pksVewvkQBhIBevjBZorRvAThvEBbX8Xe.TQuxLoTFT7u"), []byte(password+"x"))
		return nil, "", ErrBadCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(p.PasswordHash()), []byte(password)) != nil {
		return nil, "", ErrBadCredentials
	}
	tok, err := a.Issue(p.ID)
	return p, tok, err
}

// Issue signs a session token for a user id.
func (a *Auth) Issue(userID string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{Subject: userID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(a.ttl)), Issuer: "tropa"}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

// Verify parses a token and returns the user.
func (a *Auth) Verify(tok string) (*directory.Person, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) { return a.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("tropa"))
	if err != nil {
		return nil, err
	}
	p, ok := a.dir.ByID(claims.Subject)
	if !ok {
		return nil, errors.New("unknown user")
	}
	return p, nil
}

// SetCookie writes the session cookie.
func (a *Auth) SetCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: tok, Path: "/", HttpOnly: true, Secure: a.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: int(a.ttl.Seconds())})
}

// ClearCookie removes the session cookie.
func (a *Auth) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

type ctxKey struct{}

// WithUser stores the user in a context.
func WithUser(ctx context.Context, p *directory.Person) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// UserFrom returns the authenticated user (nil if none).
func UserFrom(ctx context.Context) *directory.Person {
	p, _ := ctx.Value(ctxKey{}).(*directory.Person)
	return p
}

// FromRequest resolves the user from the cookie or a Bearer token.
func (a *Auth) FromRequest(r *http.Request) (*directory.Person, error) {
	tok := ""
	if c, err := r.Cookie(CookieName); err == nil {
		tok = c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimPrefix(h, "Bearer ")
	}
	if tok == "" {
		return nil, errors.New("no session")
	}
	return a.Verify(tok)
}
