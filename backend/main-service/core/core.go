package core

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/smtp"

	"github.com/jordan-wright/email"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type CustomError struct {
	Op   string
	Msg  string
	Code int
	Err  error
}

func (e *CustomError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s | %v", e.Op, e.Err)
	}
	return fmt.Sprintf("%s", e.Op)
}

func (e *CustomError) Unwrap() error {
	return e.Err
}

func DetectFileType(file io.Reader) (io.Reader, string, error) {
	buf := make([]byte, 512)

	_, err := io.ReadFull(file, buf)
	if err != nil {
		return nil, "", &CustomError{
			Op:   "Reading first bytes of file",
			Msg:  "Unexpected error",
			Code: 500,
			Err:  err,
		}
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
		return nil, "", &CustomError{
			Op:   "Verifying format",
			Msg:  "Unallowed format",
			Code: 400,
			Err:  nil,
		}
	}
	return stream, fType, nil
}

// CriticalAlerter sends an email if Internal errors happened
// 400 or 403 and other 4xx errors are in ignoring
func CriticalAlerter(adminEmail, subject, status, errorMsg, addr, port, sender, password string) error {
	mail := email.NewEmail()
	mail.From = fmt.Sprintf("Critical Alert System <%s>", sender)
	mail.To = []string{adminEmail}
	mail.Subject = fmt.Sprintf("Subject: %s", subject)

	mail.Text = []byte(fmt.Sprintf("[%s], %s", status, errorMsg))

	address := fmt.Sprintf("%s:%s", addr, port)
	auth := smtp.PlainAuth("", sender, password, addr)

	tlsConf := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         addr,
	}

	fmt.Printf("Alert system is going to send a mail\n")

	if err := mail.SendWithStartTLS(address, auth, tlsConf); err != nil {
		return err
	}

	fmt.Printf("Alert system has done its work\n")

	return nil
}

func GrpcClient(grpcAddr string) (*grpc.ClientConn, error) {
	client, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return client, nil
}
