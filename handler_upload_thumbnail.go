package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse media Type from Content-Type header", err)
		return
	}
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "invalid media type.", err)
		return
	}

	randFileNamebytes := make([]byte, 32)
	_, err = rand.Read(randFileNamebytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "issue generating random number", err)
		return
	}
	randomIDString := base64.RawURLEncoding.EncodeToString(randFileNamebytes)

	fileExtention := strings.Split(mediaType, "/")[1]
	fileName := randomIDString + "." + fileExtention
	thumbFilePath := filepath.Join(cfg.assetsRoot, fileName)

	serverThumbFile, err := os.Create(thumbFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "issue creating thumnail file", err)
		return
	}
	defer serverThumbFile.Close()
	_, err = io.Copy(serverThumbFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "issue creating thumnail file", err)
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

	newThumbURL := fmt.Sprintf("http://localhost:%v/assets/%v.%v", cfg.port, randomIDString, fileExtention)

	updVideo := database.Video{
		ID:                videoID,
		CreatedAt:         dbVideo.CreatedAt,
		UpdatedAt:         time.Now(),
		ThumbnailURL:      &newThumbURL,
		CreateVideoParams: dbVideo.CreateVideoParams,
	}

	err = cfg.db.UpdateVideo(updVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "issue updating video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, updVideo)
}
