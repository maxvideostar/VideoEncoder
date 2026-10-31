package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type session struct {
	user    string
	expires time.Time
}

var (
	sessMu   sync.Mutex
	sessions = map[string]session{}
	failMu   sync.Mutex
	failures = map[string]*failInfo{}
)

type failInfo struct {
	count int
	until time.Time
}

const sessionCookie = "ve_session"
const sessionTTL = 7 * 24 * time.Hour

func randToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hashPass(pass, salt string) string {
	h := sha256.Sum256([]byte(salt + ":" + pass))
	for i := 0; i < 50000; i++ {
		h = sha256.Sum256(append(h[:], salt...))
	}
	return hex.EncodeToString(h[:])
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isLocal – cererea vine de pe același calculator (nu printr-un reverse proxy)
func isLocal(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" || r.Header.Get("Forwarded") != "" {
		return false
	}
	ip := net.ParseIP(clientIP(r))
	return ip != nil && ip.IsLoopback()
}

func currentUser(r *http.Request) (string, bool) {
	c := getCfg()
	if c.LocalNoAuth && isLocal(r) {
		return "local", true
	}
	ck, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	sessMu.Lock()
	defer sessMu.Unlock()
	s, ok := sessions[ck.Value]
	if !ok || time.Now().After(s.expires) {
		delete(sessions, ck.Value)
		return "", false
	}
	return s.user, true
}

func requireAuth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := currentUser(r); !ok {
			writeErr(w, http.StatusUnauthorized, "neautentificat")
			return
		}
		// protecție CSRF simplă pentru cereri care modifică
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
				writeErr(w, http.StatusForbidden, "origine invalidă")
				return
			}
		}
		h(w, r)
	})
}

func sameOrigin(origin, host string) bool {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	return strings.EqualFold(o, host)
}

func requireAuthPage(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := currentUser(r); !ok {
			if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			http.Error(w, "neautentificat", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	failMu.Lock()
	fi := failures[ip]
	if fi != nil && time.Now().Before(fi.until) {
		failMu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "Prea multe încercări. Reîncearcă peste câteva minute.")
		return
	}
	failMu.Unlock()

	var req struct{ Username, Password string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "cerere invalidă")
		return
	}
	c := getCfg()
	okUser := subtle.ConstantTimeCompare([]byte(req.Username), []byte(c.Username)) == 1
	okPass := subtle.ConstantTimeCompare([]byte(hashPass(req.Password, c.Salt)), []byte(c.PassHash)) == 1
	if !okUser || !okPass {
		failMu.Lock()
		if failures[ip] == nil {
			failures[ip] = &failInfo{}
		}
		failures[ip].count++
		if failures[ip].count >= 5 {
			failures[ip].until = time.Now().Add(5 * time.Minute)
			failures[ip].count = 0
		}
		failMu.Unlock()
		time.Sleep(700 * time.Millisecond)
		writeErr(w, http.StatusUnauthorized, "Utilizator sau parolă greșită")
		return
	}
	failMu.Lock()
	delete(failures, ip)
	failMu.Unlock()

	tok := randToken(32)
	sessMu.Lock()
	sessions[tok] = session{user: req.Username, expires: time.Now().Add(sessionTTL)}
	sessMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()), Secure: r.TLS != nil,
	})
	writeJSON(w, map[string]any{"ok": true})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(sessionCookie); err == nil {
		sessMu.Lock()
		delete(sessions, ck.Value)
		sessMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]any{"ok": true})
}

func dropAllSessions() {
	sessMu.Lock()
	sessions = map[string]session{}
	sessMu.Unlock()
}
