package main

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	/**
	handler to store video files in S3.
	Images will stay on the local file system for now.
	I recommend using the image upload handler as a reference.
	* Set an upload limit of 1 GB (1 << 30 bytes) using http.MaxBytesReader.
	* Extract the videoID from the URL path parameters and parse it as a UUID
	* Authenticate the user to get a userID
	* Get the video metadata from the database, if the user is not the video owner, return a http.StatusUnauthorized response
	* Parse the uploaded video file from the form data
		- Use (http.Request).FormFile with the key "video" to get a multipart.File in memory
		- Remember to defer closing the file with (os.File).Close - we don't want any memory leaks
	* Validate the uploaded file to ensure it's an MP4 video
		- Use mime.ParseMediaType and "video/mp4" as the MIME type
	* Save the uploaded file to a temporary file on disk.
		- Use os.CreateTemp to create a temporary file.
		- I passed in an empty string for the directory to use the system default, and the name "tubely-upload.mp4"
		- (but you can use whatever you want)
	* defer remove the temp file with os.Remove
	* defer close the temp file (defer is LIFO, so it will close before the remove)
	* io.Copy the contents over from the wire to the temp file
	* Reset the tempFile's file pointer to the beginning with .Seek(0, io.SeekStart)
		- this will allow us to read the file again from the beginning
	* Put the object into S3 using PutObject. You'll need to provide:
		- The bucket name
		- The file key. Use the same <random-32-byte-hex>.ext format as the key. e.g. 1a2b3c4d5e6f7890abcd1234ef567890.mp4
		- The file contents (body). The temp file is an os.File which implements io.Reader
		- Content type, which is the MIME type of the file.
	* Update the VideoURL of the video record in the database with the S3 bucket and key.
		- S3 URLs are in the format https://<bucket-name>.s3.<region>.amazonaws.com/<key>.
		- Make sure you use the correct region and bucket name!
	**/
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

	// Generate a random file name for the S3 key
	randomIDBytes := make([]byte, 32)
	_, err = rand.Read(randomIDBytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Issue generating random number", err)
		return
	}
	randomIDString := base64.RawURLEncoding.EncodeToString(randomIDBytes)
	s3Key := randomIDString + ".mp4"
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
