package main

import (
	"fmt"
	i "github.com/emersion/go-imap"
	m "github.com/emersion/go-message/mail"
	"github.com/loeffel-io/helper"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"time"
)

type mail struct {
	MessageID   string
	Subject     string
	From        []*i.Address
	Date        time.Time
	Text        [][]byte
	Attachments []*attachment
	Error       error
}

type attachment struct {
	Filename string
	Body     []byte
}

func (mail *mail) fetchMeta(message *i.Message) {
	mail.MessageID = message.Envelope.MessageId
	mail.Subject = message.Envelope.Subject
	mail.From = message.Envelope.From
	mail.Date = message.Envelope.Date
}

func (mail *mail) fetchBody(reader *m.Reader) error {
	var (
		bodies      [][]byte
		attachments []*attachment
	)

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
			body, err := ioutil.ReadAll(part.Body)

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

			if filename == "" {
				helper.Debug(header.ContentDisposition())
				helper.Debug(header.ContentType())
				return errors.New("attachment without filename")
			}

			body, err := ioutil.ReadAll(part.Body)

			if err != nil {
				return err
			}

			attachments = append(attachments, &attachment{
				Filename: filename,
				Body:     body,
			})
		}
	}

	mail.Text = bodies
	mail.Attachments = attachments

	return nil
}

func (mail *mail) getDirectoryName(username string) string {
	return fmt.Sprintf(
		"files/%s/%s-%d/%s",
		username, mail.Date.Month(), mail.Date.Year(), mail.From[0].HostName,
	)
}

func (mail *mail) getErrorText() string {
	return fmt.Sprintf("Error: %s\nSubject: %s\nFrom: %s\n", mail.Error.Error(), mail.Subject, mail.Date.Local())
}
