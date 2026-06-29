package core

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func DetectFileType(file io.Reader) (io.Reader, string, error) {
	buf := make([]byte, 512)

	_, err := io.ReadFull(file, buf)
	if err != nil {
		return nil, "", err
	}

	fType := http.DetectContentType(buf)

	var stream io.Reader

	switch fType {
	case "image/png", "image/jpeg", "image/webp", "application/pdf", "image/gif", "image/bmp", "image/x-icon",
		"text/html; charset=utf-8", "text/plain; charset=utf-8", "text/xml; charset=utf-8", "application/postscript",
		"application/zip", "application/x-gzip", "application/x-rar-compressed", "application/x-tar", "application/x-bzip2",
		"application/x-executable", "audio/mpeg", "audio/ogg", "audio/midi", "video/mp4", "video/webm", "video/ogg", "video/avi",
		"video/x-matroska", "video/x-flv", "audio/wave", "audio/x-wav", "font/woff", "font/woff2", "application/font-sfnt", "application/octet-stream":
		stream = io.MultiReader(bytes.NewReader(buf), file)
	default:
		return nil, "", fmt.Errorf("ERR: Filetype is unavailable %v", fType)
	}
	return stream, fType, nil
}
