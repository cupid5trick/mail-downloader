# Mail Downloader

### Requirements

- wkhtmltopdf
- date format: ISO 8601 - yyyy-MM-dd

### Usage

```bash
make install
go build -o mail-downloader
./mail-downloader -config=config.yml -from="2019-10-01" -to="2019-12-31"
```

### Config

```yaml
imap:
  username: secret@gmail.com
  password: secret
  server: imap.gmail.com
  port: 993

attachments:
  mimetypes:
    - application/pdf

mails:
  subjects:
    - invoice, amazon
    - rechnung
    - receipt
```