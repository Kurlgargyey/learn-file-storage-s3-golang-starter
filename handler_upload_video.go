package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30) // 1GB max
	videoID, err := uuid.Parse(r.PathValue("videoID"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video ID", err)
		return
	}
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	if userID == uuid.Nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	metadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}
	if metadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	metadata, err = cfg.dbVideoToSignedVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal server error", err)
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
	fastStart, err := cfg.processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}
	fastStartFile, err := os.Open(fastStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}
	defer os.Remove(fastStart)
	defer fastStartFile.Close()
	fileNameBytes := make([]byte, 32)
	rand.Read(fileNameBytes)
	fileName := hex.EncodeToString(fileNameBytes) + ".mp4"

	aspectRatio, err := cfg.getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}
	switch aspectRatio {
	case "16:9":
		fileName = "landscape/" + fileName
	case "9:16":
		fileName = "portrait/" + fileName
	default:
		fileName = "other/" + fileName
	}

	_, err = cfg.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{Bucket: aws.String(cfg.s3Bucket), Key: aws.String(fileName), Body: fastStartFile, ContentType: aws.String(videoMediaType)})
	if err != nil {
		fmt.Printf("error uploading video to S3: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}
	//videoURL := "https://" + cfg.s3Bucket + ".s3." + cfg.s3Region + ".amazonaws.com/" + fileName
	videoURL := cfg.s3Bucket + "," + fileName
	metadata.VideoURL = &videoURL
	cfg.db.UpdateVideo(metadata)
	fmt.Println("uploaded video", videoID, "by user", userID, "at URL", videoURL)
}
