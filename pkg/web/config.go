package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alex0ptr/aigude/pkg/userconfig"
)

// editorView is the User Config editor: the text of a file and what came of saving it.
type editorView struct {
	// File is the name of the file in the users folder.
	File string
	// Text is the text in the editor; after a failed save it is what the User wrote.
	Text string
	// Hash is the content of the file the text is based on.
	Hash string
	// Saved tells that the text was written.
	Saved bool
	// Problems are what is wrong in the text, with their positions.
	Problems []string
	// Conflict tells that the file changed on disk after the editor was opened.
	Conflict bool
}

// showConfig opens the editor with the file of the selected User.
func (h *Handler) showConfig(w http.ResponseWriter, r *http.Request) {
	a := accessOf(r)
	user := a.selectedUser(r)
	if user == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	file, err := h.source.File(user.ID)
	if err != nil {
		slog.Error("Reading the User Config file failed", "user", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.renderConfig(w, a, user, http.StatusOK, editorView{File: file.Name, Text: file.Text, Hash: file.Hash})
}

// saveConfig checks the text of the editor and writes it into the file of the selected User.
func (h *Handler) saveConfig(w http.ResponseWriter, r *http.Request) {
	a := accessOf(r)
	user := a.selectedUser(r)
	if user == nil {
		http.NotFound(w, r)
		return
	}
	// Browsers send the lines of a textarea separated by CRLF; the file keeps the line ending it has.
	text := strings.ReplaceAll(r.PostFormValue("text"), "\r\n", "\n")
	hash := r.PostFormValue("hash")
	file, err := h.source.File(user.ID)
	if err != nil {
		slog.Error("Reading the User Config file failed", "user", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	view := editorView{File: file.Name, Text: text, Hash: hash}

	var problems userconfig.Problems
	switch err := h.source.Save(user.ID, text, hash); {
	case err == nil:
		saved, readErr := h.source.File(user.ID)
		if readErr != nil {
			slog.Error("Reading the saved User Config file failed", "user", user.ID, "error", readErr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.renderConfig(w, a, user, http.StatusOK, editorView{File: saved.Name, Text: saved.Text, Hash: saved.Hash, Saved: true})
	case errors.Is(err, userconfig.ErrConflict):
		view.Conflict = true
		h.renderConfig(w, a, user, http.StatusConflict, view)
	case errors.As(err, &problems):
		for _, p := range problems {
			view.Problems = append(view.Problems, p.String())
		}
		h.renderConfig(w, a, user, http.StatusUnprocessableEntity, view)
	default:
		slog.Error("Saving the User Config failed", "user", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// renderConfig writes the page of the editor.
func (h *Handler) renderConfig(w http.ResponseWriter, a access, user *userconfig.User, status int, view editorView) {
	data := h.newPage(a, user)
	data.Editor = view
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.config.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("Render editor failed", "user", user.ID, "error", err)
	}
}
