package main

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Damba017/leitner/internal/auth"
)

// adminOnly answers 403 to everyone who isn't an admin. requireSession has
// already handled the logged-out case.
func (s *server) adminOnly(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := currentUser(r); !ok || !u.IsAdmin {
			http.Error(w, "Verboden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (s *server) renderAdmin(w http.ResponseWriter, r *http.Request, status int, notice, errMsg string) {
	me, _ := currentUser(r)
	users, err := auth.ListUsers(r.Context(), s.db)
	if err != nil {
		s.serverError(w, err)
		return
	}
	open, err := auth.RegistrationOpen(r.Context(), s.db)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "admin.html", pageData{
		Me: me, Nav: "admin", Users: users, RegistrationOpen: open, Notice: notice, Error: errMsg,
	})
}

func (s *server) adminPage(w http.ResponseWriter, r *http.Request) {
	s.renderAdmin(w, r, http.StatusOK, "", "")
}

// formFlag reads a "0"/"1" form value. Anything else is a 400, so a broken
// form never revokes or closes something by accident.
func formFlag(w http.ResponseWriter, r *http.Request, name string) (value, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	switch r.PostFormValue(name) {
	case "1":
		return true, true
	case "0":
		return false, true
	}
	http.Error(w, "Ongeldig formulier", http.StatusBadRequest)
	return false, false
}

func (s *server) setRegistration(w http.ResponseWriter, r *http.Request) {
	open, ok := formFlag(w, r, "open")
	if !ok {
		return
	}
	if err := auth.SetRegistrationOpen(r.Context(), s.db, open); err != nil {
		s.serverError(w, err)
		return
	}
	me, _ := currentUser(r)
	slog.Info("registration changed", "by", me.Username, "open", open)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// targetID parses the {id} path value; it writes a 404 and returns false if invalid.
func (s *server) targetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}

// adminError shows a known error on the admin page.
func (s *server) adminError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrLastAdmin):
		s.renderAdmin(w, r, http.StatusConflict, "", "Er moet minstens één admin overblijven.")
	case errors.Is(err, auth.ErrUserNotFound):
		s.renderAdmin(w, r, http.StatusNotFound, "", "Deze gebruiker bestaat niet (meer).")
	default:
		if msg, ok := passwordErrors[err]; ok {
			s.renderAdmin(w, r, http.StatusBadRequest, "", msg)
			return
		}
		s.serverError(w, err)
	}
}

func (s *server) setUserAdmin(w http.ResponseWriter, r *http.Request) {
	id, ok := s.targetID(w, r)
	if !ok {
		return
	}
	admin, ok := formFlag(w, r, "admin")
	if !ok {
		return
	}
	if err := auth.SetAdmin(r.Context(), s.db, id, admin); err != nil {
		s.adminError(w, r, err)
		return
	}
	me, _ := currentUser(r)
	slog.Info("admin rights changed", "by", me.Username, "target", id, "admin", admin)
	if id == me.ID && !admin {
		// No admin page for us any more.
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// resetUserPassword sets the password an admin typed and logs that user out
// everywhere. For the admin's own account this device stays logged in.
func (s *server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := s.targetID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	me, _ := currentUser(r)
	u, err := auth.GetUser(r.Context(), s.db, id)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	var keep string
	if c, err := r.Cookie(sessionCookie); err == nil && id == me.ID {
		keep = c.Value
	}
	if err := auth.SetPassword(r.Context(), s.db, id, r.PostFormValue("password"), keep); err != nil {
		s.adminError(w, r, err)
		return
	}
	slog.Info("password reset", "by", me.Username, "target", u.Username)
	notice := "Wachtwoord van " + u.Username + " is gewijzigd; " + u.Username + " is overal uitgelogd."
	if id == me.ID {
		notice = "Je wachtwoord is gewijzigd; je andere apparaten zijn uitgelogd."
	}
	// Rendered, not redirected, so the notice shows. Sending the form again
	// sets the same password again: harmless.
	s.renderAdmin(w, r, http.StatusOK, notice, "")
}

// otherUser parses {id} and refuses the admin's own account: deleting
// yourself on the admin page is always a mistake.
func (s *server) otherUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	id, ok := s.targetID(w, r)
	if !ok {
		return auth.User{}, false
	}
	if me, _ := currentUser(r); id == me.ID {
		s.renderAdmin(w, r, http.StatusBadRequest, "", "Je kunt je eigen account hier niet verwijderen.")
		return auth.User{}, false
	}
	u, err := auth.GetUser(r.Context(), s.db, id)
	if err != nil {
		s.adminError(w, r, err)
		return auth.User{}, false
	}
	return u, true
}

func (s *server) confirmDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.otherUser(w, r)
	if !ok {
		return
	}
	me, _ := currentUser(r)
	s.render(w, http.StatusOK, "confirm.html", pageData{Me: me, Nav: "admin", Confirm: confirmPage{
		Question: "Account " + u.Username + " verwijderen?",
		Note: u.Username + " (" + u.Email + ") wordt overal uitgelogd. Het account en alle koppen, lijsten en kaarten " +
			"ervan worden verwijderd. Dit kan niet ongedaan worden gemaakt.",
		Action: "/admin/users/" + strconv.FormatInt(u.ID, 10) + "/delete",
		Button: "Ja, verwijder " + u.Username,
		Back:   "/admin",
	}})
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.otherUser(w, r)
	if !ok {
		return
	}
	if err := auth.DeleteUser(r.Context(), s.db, u.ID); err != nil {
		s.adminError(w, r, err)
		return
	}
	me, _ := currentUser(r)
	slog.Info("user deleted", "by", me.Username, "target", u.Username)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
