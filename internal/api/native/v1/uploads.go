package v1

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

type uploadResponse struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	MIMEType  string    `json:"mime_type"`
	FileName  string    `json:"file_name"`
	Size      int64     `json:"size"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Minute))
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	limit := s.service.MaxUploadBytes() + (1 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	reader, err := r.MultipartReader()
	if err != nil {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use multipart/form-data with one file part.")
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "One file part is required.")
		return
	}
	defer part.Close()
	upload, err := s.service.CreateUpload(ctx, r.PathValue("id"), part.FileName(), part.Header.Get("Content-Type"), part)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(w, r, http.StatusRequestEntityTooLarge, "media_too_large", "Upload exceeds the configured file size limit.")
			return
		}
		respondError(w, r, err)
		return
	}
	part, err = reader.NextPart()
	if err != io.EOF {
		_ = s.service.DeleteUpload(r.Context(), upload.AccountID, upload.ID)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(w, r, http.StatusRequestEntityTooLarge, "media_too_large", "Upload exceeds the configured file size limit.")
		} else {
			if part != nil {
				part.Close()
			}
			writeProblem(w, r, http.StatusBadRequest, "invalid_input", "Send exactly one file part.")
		}
		return
	}
	w.Header().Set("Location", "/api/v1/accounts/"+upload.AccountID+"/uploads/"+upload.ID)
	writeJSON(w, http.StatusCreated, uploadResponse{ID: upload.ID, AccountID: upload.AccountID,
		MIMEType: upload.MIMEType, FileName: upload.FileName, Size: upload.Size, ExpiresAt: upload.ExpiresAt})
}

func (s *Server) deleteUpload(w http.ResponseWriter, r *http.Request) {
	if err := s.service.DeleteUpload(r.Context(), r.PathValue("id"), r.PathValue("upload_id")); err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
