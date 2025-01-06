package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func (cfg apiConfig) getVideoAspectRatio(filePath string) (string, error) {
	buf := bytes.Buffer{}
	probeCmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	probeCmd.Stdout = &buf
	err := probeCmd.Run()
	if err != nil {
		return "", err
	}
	probeJSON := struct{ Streams []struct{ Width, Height int } }{}
	json.Unmarshal(buf.Bytes(), &probeJSON)
	if len(probeJSON.Streams) == 0 {
		return "", errors.New("no streams found")
	}
	width := float64(probeJSON.Streams[0].Width)
	height := float64(probeJSON.Streams[0].Height)
	precision := 3
	switch roundFloat(width/height, precision) {
	case roundFloat(16.0/9.0, precision):
		return "16:9", nil
	case roundFloat(9.0/16.0, precision):
		return "9:16", nil
	default:
		return "other", nil
	}
}

func (cfg apiConfig) processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"
	encodeCmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	err := encodeCmd.Run()
	if err != nil {
		return "", err
	}
	return outputFilePath, nil
}

func roundFloat(f float64, precision int) float64 {
	shift := math.Pow(10, float64(precision))
	return math.Round(f*shift) / shift
}

func (cfg apiConfig) generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	presign := s3.NewPresignClient(cfg.s3Client)
	presignReq, err := presign.PresignGetObject(context.TODO(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}
	return presignReq.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	bucketAndKey := strings.SplitN(*video.VideoURL, ",", 2)

	bucket := bucketAndKey[0]
	key := bucketAndKey[1]
	signedURL, err := cfg.generatePresignedURL(cfg.s3Client, bucket, key, time.Hour)
	if err != nil {
		return database.Video{}, err
	}
	video.VideoURL = &signedURL
	return video, nil
}
