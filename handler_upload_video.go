package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	http.MaxBytesReader(w, r.Body, 1<<30) // Set upload limit to 1 GB

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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video from database", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You are not the owner of this video", nil)
		return
	}
	file, header, err := r.FormFile("video")
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
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "invalid media type. Only video/mp4 is allowed", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temporary file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy file to temporary file", err)
		return
	}
	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to seek to the beginning of the temporary file", err)
		return
	}
	// Get the aspect ratio of the video
	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video aspect ratio", err)
		return
	}
	var videoPrefix string
	switch aspectRatio {
	case "16:9":
		videoPrefix = "landscape"
	case "9:16":
		videoPrefix = "portrait"
	default:
		videoPrefix = "other"
	}
	// Generate a random file name for the S3 key
	randomIDBytes := make([]byte, 32)
	_, err = rand.Read(randomIDBytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Issue generating random number", err)
		return
	}
	randomIDString := base64.RawURLEncoding.EncodeToString(randomIDBytes)
	s3Key := videoPrefix + "/" + randomIDString + ".mp4"
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &s3Key,
		Body:        tempFile,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload video to S3", err)
		return
	}
	// S3 URLs are in the format https://<bucket-name>.s3.<region>.amazonaws.com/<key>.
	videoURL := "https://" + cfg.s3Bucket + ".s3." + cfg.s3Region + ".amazonaws.com/" + s3Key
	video.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video in database", err)
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{"videoURL": *video.VideoURL})
}

func getVideoAspectRatio(filePath string) (string, error) {
	// takes a file path and returns the aspect ratio as a string
	execCommand := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	output := bytes.Buffer{}
	execCommand.Stdout = &output
	err := execCommand.Run()
	if err != nil {
		return "", err
	}
	//Unmarshal the stdout of the command from the buffer's .Bytes
	// into a JSON struct so that you can get the width and height fields.
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return "", err
	}
	streams, ok := result["streams"].([]any)
	if !ok || len(streams) == 0 {
		return "", nil // No streams found
	}
	stream := streams[0].(map[string]any)
	widthVal, widthOk := stream["width"].(float64)
	heightVal, heightOk := stream["height"].(float64)
	if !widthOk || !heightOk {
		return "", nil // Width or height not found
	}

	return formatAspectRatio(widthVal, heightVal), nil
}

func formatAspectRatio(width float64, height float64) string {
	ratio := (width / height)
	if math.Abs(ratio-float64(16)/float64(9)) <= 0.01 {
		log.Println("Aspect ratio 16:9: " + fmt.Sprintf("%f", ratio))
		return "16:9"
	} else if math.Abs(ratio-float64(9)/float64(16)) <= 0.01 {
		log.Println("Aspect ratio 9:16: " + fmt.Sprintf("%f", ratio))
		return "9:16"
	} else {
		log.Println("Aspect ratio other: " + fmt.Sprintf("%f", ratio))
		return "other"
	}
}
