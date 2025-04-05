package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SebastiaanKlippert/go-wkhtmltopdf"
	i "github.com/emersion/go-imap"
	m "github.com/emersion/go-message/mail"
	"github.com/gabriel-vasile/mimetype"
	"github.com/loeffel-io/mail-downloader/counter"
)

type mail struct {
	Uid                uint32
	MessageID          string
	Subject            string
	From               []*i.Address
	Date               time.Time
	Body               [][]byte
	Attachments        []*attachment
	MimeType           string
	MultipartMimeType  []string
	AttachmentMimeType []string
	Error              error
}

type attachment struct {
	Filename string
	Body     []byte
	Mimetype string
}

// jsonMail is used for JSON serialization
type jsonMail struct {
	Uid                uint32    `json:"uid"`
	MessageID          string    `json:"message_id"`
	Subject            string    `json:"subject"`
	From               []string  `json:"from"`
	Date               time.Time `json:"date"`
	Attachments        []string  `json:"attachments"`
	MimeType           string    `json:"mime_type"`
	MultipartMimeType  []string  `json:"multipart_mime_type"`
	AttachmentMimeType []string  `json:"attachment_mime_type"`
}

// mailList represents the metadata for a user's email collection
type mailList struct {
	Email  string     `json:"email"`
	List   []jsonMail `json:"list"`
	Vendor string     `json:"vendor"`
	Server string     `json:"server"`
	mu     sync.Mutex `json:"-"`
	path   string     `json:"-"`
}

// newMailList creates a new mailList or loads existing one
func newMailList(email, vendor, server, mailDir string) (*mailList, error) {
	ml := &mailList{
		Email:  email,
		List:   make([]jsonMail, 0),
		Vendor: vendor,
		Server: server,
		path:   filepath.Join(mailDir, "data.json"),
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(ml.path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Try to load existing data
	data, err := os.ReadFile(ml.path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, that's fine
			return ml, nil
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	if err := json.Unmarshal(data, ml); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return ml, nil
}

// addMail adds a new mail to the list and saves the metadata
func (ml *mailList) addMail(mail *mail) error {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	// Convert mail to jsonMail
	jsonData, err := mail.toJson()
	if err != nil {
		return fmt.Errorf("failed to convert mail to json: %w", err)
	}

	var jm jsonMail
	if err := json.Unmarshal(jsonData, &jm); err != nil {
		return fmt.Errorf("failed to parse mail json: %w", err)
	}

	// Check if mail already exists
	for i, existing := range ml.List {
		if existing.Uid == jm.Uid {
			// Update existing entry
			ml.List[i] = jm
			return ml.save()
		}
	}

	// Add new entry
	ml.List = append(ml.List, jm)
	return ml.save()
}

// save writes the metadata to disk atomically
func (ml *mailList) save() error {
	data, err := json.MarshalIndent(ml, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Write to temporary file first
	tmpPath := ml.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Rename temporary file to actual file (atomic operation)
	if err := os.Rename(tmpPath, ml.path); err != nil {
		os.Remove(tmpPath) // Clean up temp file if rename fails
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

func (mail *mail) fetchMeta(message *i.Message) {
	mail.Uid = message.Uid
	mail.MessageID = message.Envelope.MessageId
	mail.Subject = message.Envelope.Subject
	mail.From = message.Envelope.From
	mail.Date = message.Envelope.Date
}

func (mail *mail) fetchBody(reader *m.Reader) error {
	var (
		bodies      [][]byte
		attachments []*attachment
		count       = counter.CreateCounter()
	)

	// Get the mail's content type
	contentType, _, err := reader.Header.ContentType()
	if err == nil {
		mail.MimeType = contentType
	}

	for {
		part, err := reader.NextPart()
		if err != nil {
			if err == io.EOF || err.Error() == "multipart: NextPart: EOF" {
				break
			}

			return err
		}

		switch header := part.Header.(type) {
		case *m.InlineHeader:
			body, err := io.ReadAll(part.Body)
			if err != nil {
				if err == io.ErrUnexpectedEOF {
					continue
				}

				return err
			}

			bodies = append(bodies, body)
		case *m.AttachmentHeader:
			// This is an attachment
			filename, err := header.Filename()
			if err != nil {
				return err
			}

			body, err := io.ReadAll(part.Body)
			if err != nil {
				return err
			}

			mime := mimetype.Detect(body)

			if filename == "" {
				filename = fmt.Sprintf("%d-%d%s", mail.Uid, count.Next(), mime.Extension())
			}

			filename = new(imap).fixUtf(filename)

			// Replace all slashes with dashes to prevent directory traversal
			filename = strings.ReplaceAll(filename, "/", "-")

			attachments = append(attachments, &attachment{
				Filename: filename,
				Body:     body,
				Mimetype: mime.String(),
			})
		}
	}

	mail.Body = bodies
	mail.Attachments = attachments

	return nil
}

func (mail *mail) generatePdf() ([]byte, error) {
	count := counter.CreateCounter()

	pdfg, err := wkhtmltopdf.NewPDFGenerator()
	if err != nil {
		return nil, err
	}

	pdfg.LowQuality.Set(true)
	pdfg.Orientation.Set(wkhtmltopdf.OrientationPortrait)
	pdfg.PageSize.Set(wkhtmltopdf.PageSizeA4)

	for _, body := range mail.Body {
		if mime := mimetype.Detect(body); !mime.Is("text/html") {
			continue
		}

		page := wkhtmltopdf.NewPageReader(bytes.NewReader(body))
		page.DisableJavascript.Set(true)
		page.Encoding.Set("UTF-8")

		pdfg.AddPage(page)
		count.Next()
	}

	if count.Current() == 0 {
		return nil, nil
	}

	if err := pdfg.Create(); err != nil {
		return nil, err
	}

	return pdfg.Bytes(), nil
}

func (mail *mail) getDirectoryName(root, username string) string {
	return fmt.Sprintf(
		"%s/%s/%s/%s",
		root, username, mail.Date.Format("200601"), mail.From[0].HostName,
	)
}

func (mail *mail) getErrorText() string {
	return fmt.Sprintf("Error: %s\nSubject: %s\nFrom: %s\n", mail.Error.Error(), mail.Subject, mail.Date)
}

func (mail *mail) toJson() ([]byte, error) {
	// Convert mail.From to string slice
	fromAddrs := make([]string, len(mail.From))
	for i, addr := range mail.From {
		if addr.PersonalName != "" {
			fromAddrs[i] = fmt.Sprintf("%s <%s@%s>", addr.PersonalName, addr.MailboxName, addr.HostName)
		} else {
			fromAddrs[i] = fmt.Sprintf("%s@%s", addr.MailboxName, addr.HostName)
		}
	}

	// Convert attachments to filename slice
	attachmentNames := make([]string, len(mail.Attachments))
	for i, att := range mail.Attachments {
		attachmentNames[i] = att.Filename
	}

	// Create JSON structure
	jsonData := jsonMail{
		Uid:                mail.Uid,
		MessageID:          mail.MessageID,
		Subject:            mail.Subject,
		From:               fromAddrs,
		Date:               mail.Date,
		Attachments:        attachmentNames,
		MimeType:           mail.MimeType,
		MultipartMimeType:  mail.MultipartMimeType,
		AttachmentMimeType: mail.AttachmentMimeType,
	}

	// Use json.Marshal with SetEscapeHTML(false) to preserve unicode and compact output
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "")
	if err := encoder.Encode(jsonData); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
