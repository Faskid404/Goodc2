package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func jwtSecret() []byte {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		s = "c2-jwt-secret-change-in-prod"
	}
	return []byte(s)
}

func agentToken() string {
	t := os.Getenv("AGENT_TOKEN")
	if t == "" {
		return "c2-agent-token-change-in-prod"
	}
	return t
}

func dashPassword() string {
	p := os.Getenv("DASH_PASSWORD")
	if p == "" {
		return "omowoli12345"
	}
	return p
}

type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func issueToken(role string, ttl time.Duration) (string, error) {
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret())
}

func parseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret(), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(h, "Bearer "), true
}

func middlewareJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw string
		// Support token in query string for WebSocket upgrades
		if q := r.URL.Query().Get("token"); q != "" {
			raw = q
		} else {
			var ok bool
			raw, ok = bearerToken(r)
			if !ok {
				jsonError(w, "missing authorization", http.StatusUnauthorized)
				return
			}
		}
		claims, err := parseToken(raw)
		if err != nil {
			jsonError(w, "invalid token", http.StatusUnauthorized)
			return
		}
		if claims.Role != "dashboard" {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func middlewareAgentToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Agent-Token") != agentToken() {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
