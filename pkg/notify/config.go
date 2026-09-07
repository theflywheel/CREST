package notify

import "os"

// Configured builds the notification sender selected by deployment settings.
func Configured() (Sender, error) {
	if addr, from := os.Getenv("SMTP_ADDR"), os.Getenv("SMTP_FROM"); addr != "" || from != "" {
		return NewSMTP(SMTPConfig{Addr: addr, From: from, Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD")})
	}
	if endpoint := os.Getenv("NOTIFY_HTTP_URL"); endpoint != "" {
		return NewHTTP(HTTPConfig{URL: endpoint, Token: os.Getenv("NOTIFY_HTTP_TOKEN")})
	}
	return nil, nil
}
