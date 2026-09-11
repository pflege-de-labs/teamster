package httpserver

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// languageCookie remembers a choice made in the UI. It is not a session: a
// language preference survives signing out, and someone reading the login page
// in the wrong language has nowhere else to put it.
const languageCookie = "teamster_language"

// languageYear is how long the choice lasts. Long, because nobody wants to
// pick their language again every week, and harmless, because it decides
// nothing but wording.
const languageYear = 365 * 24 * time.Hour

// handleLanguage records a choice and returns to the page it was made on.
func (s *Server) handleLanguage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}

	chosen := r.PostFormValue("language")
	// "" is a deliberate choice too: it means "whatever the browser asks for",
	// and clearing the cookie is how that is stored.
	if chosen == "" {
		http.SetCookie(w, &http.Cookie{
			Name: languageCookie, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: isTLS(r), SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, returnTo(r), http.StatusSeeOther)
		return
	}

	// Only a language this build can actually render, and stored as the tag
	// rather than as whatever was typed.
	tag, ok := s.text.Supported(chosen)
	if !ok {
		http.Error(w, "unknown language", http.StatusBadRequest)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: languageCookie, Value: tag.String(), Path: "/",
		Expires: time.Now().Add(languageYear),
		MaxAge:  int(languageYear.Seconds()),
		// The cookie is read by the server, never by a script.
		HttpOnly: true, Secure: isTLS(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, returnTo(r), http.StatusSeeOther)
}

// returnTo keeps the operator where they were. A path only, and one that
// cannot be read as another host: an open redirect is an open redirect even
// when it is only about language.
func returnTo(r *http.Request) string {
	target := r.PostFormValue("return")
	if target == "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "/admin"
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host != "" || parsed.Scheme != "" {
		return "/admin"
	}
	return parsed.RequestURI()
}

// languagePreference is the order: what the operator picked, then what the
// browser asks for, then what the deployment configured. The second return says
// whether the choice was theirs, so the picker can show "browser default" as
// selected when it is.
func (s *Server) languagePreference(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie(languageCookie); err == nil && cookie.Value != "" {
		if tag, ok := s.text.Supported(cookie.Value); ok {
			return tag.String(), true
		}
	}
	return s.text.Match(r.Header.Get("Accept-Language")).String(), false
}

// languageTags is what the picker offers, in the order the bundle keeps them.
func (s *Server) languageTags() []string {
	tags := s.text.Languages()
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, tag.String())
	}
	sort.Strings(out)
	return out
}
