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

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
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
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse multipart form", err)
		return
	}
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()
	mediaType := header.Header.Get("Content-Type")
	mt, _, err := mime.ParseMediaType(mediaType)
	if mt != "image/jpg" && mt != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type for thumbnail, must be jpg or png", err)
		return
	}
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video by videoID", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User unauthorized for current video", err)
		return
	}
	mediaTypeSplit := strings.Split(mediaType, "/")
	fileExt := mediaTypeSplit[len(mediaTypeSplit)-1]
	key := make([]byte, 32)
	rand.Read(key)
	fileKey := base64.RawURLEncoding.EncodeToString(key)
	fileName := filepath.Join(cfg.assetsRoot, fileKey) + "." + fileExt
	thumbnail, err := os.Create(fileName)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to create thumbnail file", err)
		return
	}
	_, err = io.Copy(thumbnail, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to copy thumbnail to new file", err)
		return
	}
	dataUrl := fmt.Sprintf("http://localhost:%s/assets/%s.%s", cfg.port, fileKey, fileExt)
	video.ThumbnailURL = &dataUrl
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error updating video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
