package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()
	mediaType := header.Header.Get("Content-Type")
	filebytes, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "issue with converting file to bytes", err)
		return
	}

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "video not found", err)
		return
	}
	if userID != dbVideo.UserID {
		respondWithError(w, http.StatusUnauthorized, "not users video", err)
		return
	}

	thumbnail := thumbnail{
		data:      filebytes,
		mediaType: mediaType,
	}

	videoThumbnails[videoID] = thumbnail
	thumbnailURL := fmt.Sprintf("http://localhost:%v/api/thumbnails/%s", cfg.port, videoID)
	updVideo := database.Video{
		ID:                videoID,
		CreatedAt:         dbVideo.CreatedAt,
		UpdatedAt:         time.Now(),
		ThumbnailURL:      &thumbnailURL,
		CreateVideoParams: dbVideo.CreateVideoParams,
	}

	err = cfg.db.UpdateVideo(updVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "issue updating video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, updVideo)
}
