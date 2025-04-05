package main

import (
	"log"
	"strings"
	"time"
	"unicode/utf8"

	i "github.com/emersion/go-imap"
	id "github.com/emersion/go-imap-id"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/charset"
	m "github.com/emersion/go-message/mail"
	"github.com/gabriel-vasile/mimetype"
	"github.com/pkg/errors"
	"golang.org/x/text/encoding/charmap"
)

type imap struct {
	Username string
	Password string
	Server   string
	Port     string
	Client   *client.Client
}

func (imap *imap) connect() error {
	c, err := client.DialTLS(imap.Server+":"+imap.Port, nil)
	if err != nil {
		return err
	}

	// Enable ID extension support
	idClient := id.NewClient(c)
	_, err = idClient.ID(map[string]string{
		id.FieldName:    "mail-downloader",
		id.FieldVersion: "1.0.0",
	})
	if err != nil {
		return err
	}

	imap.Client = c
	return nil
}

func (imap *imap) login() error {
	return imap.Client.Login(imap.Username, imap.Password)
}

func (imap *imap) selectMailbox(mailbox string) (*i.MailboxStatus, error) {
	return imap.Client.Select(mailbox, true)
}

func (imap *imap) search(from, to time.Time) ([]uint32, error) {
	search := i.NewSearchCriteria()
	search.Since = from
	search.Before = to

	return imap.Client.UidSearch(search)
}

func (imap *imap) createSeqSet(uids []uint32) *i.SeqSet {
	seqset := new(i.SeqSet)
	seqset.AddNum(uids...)

	return seqset
}

func (imap *imap) enableCharsetReader() {
	charset.RegisterEncoding("ansi", charmap.Windows1252)
	charset.RegisterEncoding("iso8859-15", charmap.ISO8859_15)
	i.CharsetReader = charset.Reader
}

func (imap *imap) fixUtf(str string) string {
	callable := func(r rune) rune {
		if r == utf8.RuneError {
			return -1
		}

		return r
	}

	return strings.Map(callable, str)
}

func (imap *imap) fetchMessages(seqset *i.SeqSet, mailsChan chan *mail) error {
	messages := make(chan *i.Message)
	section := new(i.BodySectionName)
	items := []i.FetchItem{
		section.FetchItem(),
		i.FetchEnvelope,
		i.FetchBody,
		i.FetchBodyStructure,
	}

	go func() {
		if err := imap.Client.UidFetch(seqset, items, messages); err != nil {
			log.Println(err)
		}
	}()

	for message := range messages {
		mail := new(mail)
		mail.fetchMeta(message)

		// Get MIME type from the message structure
		if message.BodyStructure != nil {
			mail.MimeType = message.BodyStructure.MIMEType + "/" + message.BodyStructure.MIMESubType
		}

		reader := message.GetBody(section)

		if reader == nil {
			return errors.New("no reader")
		}

		mailReader, err := m.CreateReader(reader)

		if err != nil {
			mail.Error = err
			mailsChan <- mail

			if mailReader != nil {
				if err := mailReader.Close(); err != nil {
					log.Fatal(err)
				}
			}

			continue
		}

		// Initialize MultipartMimeType and AttachmentMimeType slices before fetching body
		mail.MultipartMimeType = make([]string, 0)
		mail.AttachmentMimeType = make([]string, 0)

		mail.Error = mail.fetchBody(mailReader)

		// Detect MIME types for each body part
		for _, body := range mail.Body {
			if len(body) > 0 {
				mtype := mimetype.Detect(body)
				if mtype != nil {
					mail.MultipartMimeType = append(mail.MultipartMimeType, mtype.String())
				}
			}
		}

		// Detect MIME types for each attachment
		for _, attachment := range mail.Attachments {
			if len(attachment.Body) > 0 {
				mtype := mimetype.Detect(attachment.Body)
				if mtype != nil {
					mail.AttachmentMimeType = append(mail.AttachmentMimeType, mtype.String())
				}
			}
		}

		mailsChan <- mail

		if mailReader != nil {
			if err := mailReader.Close(); err != nil {
				log.Fatal(err)
			}
		}
	}

	return nil
}
