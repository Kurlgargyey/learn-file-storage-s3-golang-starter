package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	http.MaxBytesReader(w, r.Body, 1<<30) // 1GB max
	videoID, err := uuid.Parse(r.PathValue("videoID"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video ID", err)
		return
	}
	user, err := cfg.db.GetUserByRefreshToken(r.Header.Get("Authorization"))
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	userID := user.ID
	if user.ID == uuid.Nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	metadata, err := cfg.db.GetVideo(videoID)
	if metadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	videoFile, videoFileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video", err)
		return
	}
	defer videoFile.Close()
	videoMediaType, _, err := mime.ParseMediaType(videoFileHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", err)
		return
	}
	if videoMediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", nil)
		return
	}
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()
	_, err = io.Copy(tempFile, videoFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}
	tempFile.Seek(0, io.SeekStart)
	fileNameBytes := make([]byte, 32)
	rand.Read(fileNameBytes)
	fileName := hex.EncodeToString(fileNameBytes) + ".mp4"
	cfg.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{Bucket: aws.String(cfg.s3Bucket), Key: aws.String(fileName), Body: tempFile, ContentType: aws.String(videoMediaType)})
	*metadata.VideoURL = "https://" + cfg.s3Bucket + ".s3." + cfg.s3Region + ".amazonaws.com/" + fileName
	cfg.db.UpdateVideo(metadata)
}
