package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxUpload = 1 << 30
	file := http.MaxBytesReader(w, r.Body, int64(maxUpload))

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
		respondWithError(w, http.StatusBadRequest, "Unable to get video by videoID", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User unauthorized for current video", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()
	mediaType := header.Header.Get("Content-Type")
	mt, _, err := mime.ParseMediaType(mediaType)
	if mt != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid type for video upload, must be mp4", err)
		return
	}
	fileExt := "mp4"
	tmp, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error creating temp file for upload", err)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	_, err = io.Copy(tmp, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error reading file to temp file", err)
		return
	}
	tmp.Seek(0, io.SeekStart)
	processedVideoName, err := processVideoForFastStart(tmp.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error processing video for fast start", err)
		return
	}
	processedVideo, err := os.Open(processedVideoName)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error opening processed video to upload", err)
		return
	}
	defer os.Remove(processedVideo.Name())
	defer processedVideo.Close()
	hexString, err := GenerateRandomHexString(32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error creating hex string for file name", err)
		return
	}
	aspectRatio, err := getVideoAspectRatio(tmp.Name())
	var prefix string
	switch aspectRatio {
	case "16:9":
		prefix = "landscape/"
	case "9:16":
		prefix = "portrait/"
	default:
		prefix = "other/"
	}
	keyString := prefix + hexString + "." + fileExt

	objectParams := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &keyString,
		Body:        processedVideo,
		ContentType: &mt,
	}
	_, err = cfg.s3Client.PutObject(context.TODO(), &objectParams)
	videoUrl := cfg.s3CfDistribution + keyString
	video.VideoURL = &videoUrl
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error updating video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, video)
}

func GenerateRandomHexString(byteLength int) (string, error) {
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func getVideoAspectRatio(filepath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filepath)
	var stdOut, stdErr bytes.Buffer
	cmd.Stdout = &stdOut
	cmd.Stderr = &stdErr
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Command failed: %v\nCommand error: %v\n", err, stdErr.String())
	}
	type videoData struct {
		Streams []struct {
			Index              int    `json:"index"`
			CodecName          string `json:"codec_name,omitempty"`
			CodecLongName      string `json:"codec_long_name,omitempty"`
			Profile            string `json:"profile,omitempty"`
			CodecType          string `json:"codec_type"`
			CodecTagString     string `json:"codec_tag_string"`
			CodecTag           string `json:"codec_tag"`
			Width              int    `json:"width,omitempty"`
			Height             int    `json:"height,omitempty"`
			CodedWidth         int    `json:"coded_width,omitempty"`
			CodedHeight        int    `json:"coded_height,omitempty"`
			ClosedCaptions     int    `json:"closed_captions,omitempty"`
			FilmGrain          int    `json:"film_grain,omitempty"`
			HasBFrames         int    `json:"has_b_frames,omitempty"`
			SampleAspectRatio  string `json:"sample_aspect_ratio,omitempty"`
			DisplayAspectRatio string `json:"display_aspect_ratio,omitempty"`
			PixFmt             string `json:"pix_fmt,omitempty"`
			Level              int    `json:"level,omitempty"`
			ColorRange         string `json:"color_range,omitempty"`
			ColorSpace         string `json:"color_space,omitempty"`
			ColorTransfer      string `json:"color_transfer,omitempty"`
			ColorPrimaries     string `json:"color_primaries,omitempty"`
			ChromaLocation     string `json:"chroma_location,omitempty"`
			FieldOrder         string `json:"field_order,omitempty"`
			Refs               int    `json:"refs,omitempty"`
			IsAvc              string `json:"is_avc,omitempty"`
			NalLengthSize      string `json:"nal_length_size,omitempty"`
			ID                 string `json:"id"`
			RFrameRate         string `json:"r_frame_rate"`
			AvgFrameRate       string `json:"avg_frame_rate"`
			TimeBase           string `json:"time_base"`
			StartPts           int    `json:"start_pts"`
			StartTime          string `json:"start_time"`
			DurationTs         int    `json:"duration_ts"`
			Duration           string `json:"duration"`
			BitRate            string `json:"bit_rate,omitempty"`
			BitsPerRawSample   string `json:"bits_per_raw_sample,omitempty"`
			NbFrames           string `json:"nb_frames"`
			ExtradataSize      int    `json:"extradata_size"`
			Disposition        struct {
				Default         int `json:"default"`
				Dub             int `json:"dub"`
				Original        int `json:"original"`
				Comment         int `json:"comment"`
				Lyrics          int `json:"lyrics"`
				Karaoke         int `json:"karaoke"`
				Forced          int `json:"forced"`
				HearingImpaired int `json:"hearing_impaired"`
				VisualImpaired  int `json:"visual_impaired"`
				CleanEffects    int `json:"clean_effects"`
				AttachedPic     int `json:"attached_pic"`
				TimedThumbnails int `json:"timed_thumbnails"`
				NonDiegetic     int `json:"non_diegetic"`
				Captions        int `json:"captions"`
				Descriptions    int `json:"descriptions"`
				Metadata        int `json:"metadata"`
				Dependent       int `json:"dependent"`
				StillImage      int `json:"still_image"`
				Multilayer      int `json:"multilayer"`
			} `json:"disposition"`
			Tags struct {
				Language    string `json:"language"`
				HandlerName string `json:"handler_name"`
				VendorID    string `json:"vendor_id"`
				Encoder     string `json:"encoder"`
				Timecode    string `json:"timecode"`
			} `json:"tags"`
			SampleFmt      string `json:"sample_fmt,omitempty"`
			SampleRate     string `json:"sample_rate,omitempty"`
			Channels       int    `json:"channels,omitempty"`
			ChannelLayout  string `json:"channel_layout,omitempty"`
			BitsPerSample  int    `json:"bits_per_sample,omitempty"`
			InitialPadding int    `json:"initial_padding,omitempty"`
		} `json:"streams"`
	}
	var vidData videoData
	err = json.Unmarshal(stdOut.Bytes(), &vidData)
	if err != nil {
		log.Fatalf("Failed to unmarshal JSON: %v", err)
	}
	var width, height int

	for _, stream := range vidData.Streams {
		if stream.CodecType == "video" {
			width = stream.Width
			height = stream.Height
			break
		}
	}

	if height == 0 {
		return "", fmt.Errorf("no video stream found in file")
	}

	aspectRatio := float64(width) / float64(height)

	var aspect string
	if aspectRatio > 1.7 && aspectRatio < 1.8 {
		aspect = "16:9"
	} else if aspectRatio > 0.5 && aspectRatio < 0.6 {
		aspect = "9:16"
	} else {
		aspect = "other"
	}

	return aspect, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	var stdOut, stdErr bytes.Buffer
	cmd.Stdout = &stdOut
	cmd.Stderr = &stdErr
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Command failed: %v\nCommand error: %v\n", err, stdErr.String())
	}
	return outputFilePath, nil
}
