package mail

import (
	"fmt"
	"net/smtp"
	"net/textproto"

	"github.com/jordan-wright/email"
)

type MailClient struct {
	From            string
	ServiceAuthCode string
	ServiceDomain   string
	ServicePort     string
}

func (m *MailClient) SendEmail(recipients []string, subject string, text, html []byte) error {
	e := &email.Email{
		From:    m.From,
		To:      recipients,
		Subject: subject,
		Text:    text,
		HTML:    html,
		Headers: textproto.MIMEHeader{},
	}

	auth := smtp.PlainAuth("", m.From, m.ServiceAuthCode, m.ServiceDomain)
	serviceAddr := fmt.Sprintf("%s:%s", m.ServiceDomain, m.ServicePort)

	return e.Send(serviceAddr, auth)
}
