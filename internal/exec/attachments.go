package exec

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

func resolveRunAttachments(opts Options) (Attachments, error) {
	rootDir, err := resolveWorkdir(opts.Workdir)
	if err != nil {
		return Attachments{}, err
	}
	attachments := Attachments{
		Images: append([]appserver.TurnStartImage(nil), opts.Attachments.Images...),
		Files:  append([]appserver.TurnStartFile(nil), opts.Attachments.Files...),
	}
	for _, path := range opts.ImagePaths {
		image, err := loadTurnImage(rootDir, path)
		if err != nil {
			return Attachments{}, err
		}
		attachments.Images = append(attachments.Images, image)
	}
	for _, path := range opts.FilePaths {
		file, err := loadTurnFile(rootDir, path)
		if err != nil {
			return Attachments{}, err
		}
		attachments.Files = append(attachments.Files, file)
	}
	return attachments, nil
}

func loadTurnImage(rootDir, inputPath string) (appserver.TurnStartImage, error) {
	data, absPath, err := readAttachmentFile(rootDir, inputPath)
	if err != nil {
		return appserver.TurnStartImage{}, err
	}
	mediaType := detectMediaType(absPath, data)
	if !strings.HasPrefix(mediaType, "image/") {
		return appserver.TurnStartImage{}, fmt.Errorf("--image %s has media type %s, expected image/*", inputPath, mediaType)
	}
	return appserver.TurnStartImage{
		MediaType: mediaType,
		Data:      base64.StdEncoding.EncodeToString(data),
	}, nil
}

func loadTurnFile(rootDir, inputPath string) (appserver.TurnStartFile, error) {
	data, absPath, err := readAttachmentFile(rootDir, inputPath)
	if err != nil {
		return appserver.TurnStartFile{}, err
	}
	return appserver.TurnStartFile{
		MediaType: detectMediaType(absPath, data),
		Data:      base64.StdEncoding.EncodeToString(data),
		Filename:  filepath.Base(absPath),
	}, nil
}

func readAttachmentFile(rootDir, inputPath string) ([]byte, string, error) {
	path := strings.TrimSpace(inputPath)
	if path == "" {
		return nil, "", fmt.Errorf("attachment path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootDir, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", fmt.Errorf("resolve attachment path %q: %w", inputPath, err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, "", fmt.Errorf("stat attachment %q: %w", inputPath, err)
	}
	if info.IsDir() {
		return nil, "", fmt.Errorf("attachment %q is a directory", inputPath)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, "", fmt.Errorf("read attachment %q: %w", inputPath, err)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("attachment %q is empty", inputPath)
	}
	return data, absPath, nil
}

func detectMediaType(absPath string, data []byte) string {
	if extType := mime.TypeByExtension(strings.ToLower(filepath.Ext(absPath))); extType != "" {
		if mediaType, _, err := mime.ParseMediaType(extType); err == nil && mediaType != "" {
			return mediaType
		}
	}
	return http.DetectContentType(data)
}
