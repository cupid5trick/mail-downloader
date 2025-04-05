package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cheggaaa/pb/v3"
	"gopkg.in/yaml.v3"
)

func main() {
	var config *Config

	// flags
	configPath := flag.String("config", "", "config path")
	from := flag.String("from", "", "from date")
	to := flag.String("to", "", "to date")
	flag.Parse()

	// yaml
	yamlBytes, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	// yaml to config
	err = yaml.Unmarshal(yamlBytes, &config)
	if err != nil {
		log.Fatal(err)
	}

	// imap
	imap := &imap{
		Username: config.Imap.Username,
		Password: config.Imap.Password,
		Server:   config.Imap.Server,
		Port:     config.Imap.Port,
	}

	if err := imap.connect(); err != nil {
		log.Fatal(err)
	}

	if err := imap.login(); err != nil {
		log.Fatal(err)
	}

	imap.enableCharsetReader()

	// Mailbox
	_, err = imap.selectMailbox("INBOX")
	if err != nil {
		log.Fatal(err)
	}

	// search uids
	fromDate, err := time.Parse("2006-01-02", *from)
	if err != nil {
		log.Fatal(err)
	}

	toDate, err := time.Parse("2006-01-02", *to)
	if err != nil {
		log.Fatal(err)
	}

	uids, err := imap.search(fromDate, toDate)
	if err != nil {
		log.Fatal(err)
	}

	// seqset
	seqset := imap.createSeqSet(uids)

	// channel
	mailsChan := make(chan *mail)

	// fetch messages
	go func() {
		if err = imap.fetchMessages(seqset, mailsChan); err != nil {
			log.Fatal(err)
		}
		close(mailsChan)
	}()

	// start bar
	fmt.Println("Fetching messages...")
	bar := pb.StartNew(len(uids))

	// mails
	mails := make([]*mail, 0)

	// Create mail list for metadata tracking
	mailRoot := fmt.Sprintf("mail/%s", imap.Username)
	mailList, err := newMailList(imap.Username, "imap", imap.Server, mailRoot)
	if err != nil {
		log.Fatal(err)
	}

	// fetch messages
	for mail := range mailsChan {
		mails = append(mails, mail)
		if mail.Error == nil {
			if err := mailList.addMail(mail); err != nil {
				log.Printf("Failed to update metadata for mail %d: %v", mail.Uid, err)
			}
		}
		bar.Increment()
	}

	// logout
	if err := imap.Client.Logout(); err != nil {
		log.Fatal(err)
	}

	// Initialize handlers
	handlers := []MailHandler{
		NewAttachmentHandler(imap.Username),
		NewPDFHandler(imap.Username),
		NewTextHandler(imap.Username),
	}

	// start bar
	fmt.Println("Processing messages...")
	bar.SetCurrent(0)

	// process messages
	for _, mail := range mails {
		if mail.Error != nil {
			log.Println(mail.getErrorText())
			bar.Increment()
			continue
		}

		// Process mail with all handlers
		for _, handler := range handlers {
			if err := handler.Handle(config, mail); err != nil {
				log.Printf("Handler error for mail %d: %v", mail.Uid, err)
			}
		}

		bar.Increment()
	}

	// done
	fmt.Println("Done")
}
