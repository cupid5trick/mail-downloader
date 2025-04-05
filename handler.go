package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"syscall"

	"github.com/gabriel-vasile/mimetype"
	"github.com/loeffel-io/mail-downloader/search"
)

// MailHandler interface defines the contract for mail content handlers
type MailHandler interface {
	Handle(config *Config, mail *mail) error
}

// AttachmentHandler handles saving email attachments
type AttachmentHandler struct {
	username string
}

func NewAttachmentHandler(username string) *AttachmentHandler {
	return &AttachmentHandler{username: username}
}

func (h *AttachmentHandler) Handle(config *Config, mail *mail) error {
	dir := mail.getDirectoryName("attachment", h.username)

	for _, attachment := range mail.Attachments {
		s := &search.Search{
			Search: config.Attachments.Mimetypes,
			Data:   attachment.Mimetype,
		}

		if !s.Find() {
			continue
		}

		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		if err := os.WriteFile(fmt.Sprintf("%s/%s", dir, attachment.Filename), attachment.Body, 0o644); err != nil {
			if pe, ok := err.(*os.PathError); ok {
				if pe.Err == syscall.ENAMETOOLONG {
					log.Println("path too long: " + err.Error())
					continue
				}
			}
			return fmt.Errorf("failed to write attachment: %w", err)
		}
	}

	return nil
}

// PDFHandler handles generating and saving email PDFs
type PDFHandler struct {
	username string
}

func NewPDFHandler(username string) *PDFHandler {
	return &PDFHandler{username: username}
}

func (h *PDFHandler) Handle(config *Config, mail *mail) error {
	s := &search.Search{
		Search: config.Mails.Subjects,
		Data:   mail.Subject,
	}

	if !s.Find() {
		return nil
	}

	bytes, err := mail.generatePdf()
	if err != nil {
		return fmt.Errorf("failed to generate PDF: %w", err)
	}

	if bytes == nil {
		return nil
	}

	mailDir := mail.getDirectoryName("mail", h.username)
	if err := os.MkdirAll(mailDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	safeSubject := sanitizeSubject(mail.Subject)
	pdfFilename := fmt.Sprintf("%s/%s-%s-%d.pdf",
		mailDir,
		safeSubject,
		mail.Date.Format("2006-01-02"),
		mail.Uid,
	)

	if err = os.WriteFile(pdfFilename, bytes, 0o644); err != nil {
		return fmt.Errorf("failed to write PDF: %w", err)
	}

	return nil
}

// TextHandler handles saving email text content
type TextHandler struct {
	username string
}

func NewTextHandler(username string) *TextHandler {
	return &TextHandler{username: username}
}

func mimeType(mime string) string {
	t := strings.Split(mime, ";")
	return strings.TrimSpace(t[0])
}

func (h *TextHandler) Handle(config *Config, mail *mail) error {
	// Skip if no text content
	if len(mail.Body) == 0 {
		return nil
	}

	dir := mail.getDirectoryName("mail", h.username)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	safeSubject := sanitizeSubject(mail.Subject)
	baseFilename := fmt.Sprintf("%s-%s-%d",
		safeSubject,
		mail.Date.Format("2006-01-02"),
		mail.Uid,
	)

	// Check MIME type and save appropriate content
	switch mimeType(mail.MimeType) {
	case "text/plain":
		filename := fmt.Sprintf("%s/%s.txt", dir, baseFilename)
		if err := os.WriteFile(filename, mail.Body[0], 0o644); err != nil {
			return fmt.Errorf("failed to write text file: %w", err)
		}
	case "text/html":
		filename := fmt.Sprintf("%s/%s.html", dir, baseFilename)
		if err := os.WriteFile(filename, mail.Body[0], 0o644); err != nil {
			return fmt.Errorf("failed to write HTML file: %w", err)
		}
	case "multipart/alternative", "multipart/mixed":
		// For multipart messages, use detected MIME types
		for i, body := range mail.Body {
			mime := mimetype.Detect(body)
			detectedMimeType := mime.String()

			switch mimeType(detectedMimeType) {
			case "text/plain":
				filename := fmt.Sprintf("%s/%s.txt", dir, baseFilename)
				if err := os.WriteFile(filename, body, 0o644); err != nil {
					return fmt.Errorf("failed to write text file: %w", err)
				}
			case "text/html":
				filename := fmt.Sprintf("%s/%s.html", dir, baseFilename)
				if err := os.WriteFile(filename, body, 0o644); err != nil {
					return fmt.Errorf("failed to write HTML file: %w", err)
				}
			default:
				log.Printf("Skipping unknown content type for body part %d: %s", i, detectedMimeType)
			}
		}
	default:
		// Skip other MIME types
		return nil
	}

	return nil
}

// sanitizeSubject removes or replaces unsafe characters in email subjects
func sanitizeSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	unsafeChars := regexp.MustCompile(`[\/\\:*?"<>|]`)
	return unsafeChars.ReplaceAllString(subject, "_")
}
