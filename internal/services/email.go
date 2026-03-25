package services

import (
	"fmt"
	"net/smtp"
	"strings"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// EmailService handles sending emails via SMTP
type EmailService struct {
	config *config.SMTPConfig
	logger *logger.Logger
}

// NewEmailService creates a new email service
func NewEmailService(cfg *config.SMTPConfig, log *logger.Logger) *EmailService {
	return &EmailService{
		config: cfg,
		logger: log.WithService("email_service"),
	}
}

// SendInvitationEmail sends an email invitation to share a notebook
func (s *EmailService) SendInvitationEmail(toEmail, inviterName, notebookName, permission, message, token string) error {
	if !s.config.Enabled {
		s.logger.Warn("SMTP disabled, skipping invitation email",
			zap.String("to", toEmail),
			zap.String("notebook", notebookName),
		)
		return nil
	}

	signupURL := fmt.Sprintf("%s/signup?invite=%s", s.config.AppBaseURL, token)
	loginURL := fmt.Sprintf("%s/invitations/accept?token=%s", s.config.AppBaseURL, token)

	subject := fmt.Sprintf("%s invited you to collaborate on \"%s\"", inviterName, notebookName)

	personalMessage := ""
	if message != "" {
		personalMessage = fmt.Sprintf(`
		<div style="background:#f0f4f8;border-left:4px solid #3b82f6;padding:12px 16px;margin:16px 0;border-radius:0 4px 4px 0;">
			<p style="margin:0;color:#475569;font-style:italic;">"%s"</p>
		</div>`, message)
	}

	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;max-width:600px;margin:0 auto;padding:20px;color:#1e293b;">
	<div style="text-align:center;padding:24px 0;border-bottom:1px solid #e2e8f0;">
		<h1 style="margin:0;color:#0f172a;font-size:24px;">Aether</h1>
	</div>
	<div style="padding:24px 0;">
		<p style="font-size:16px;">Hi there,</p>
		<p style="font-size:16px;"><strong>%s</strong> has invited you to collaborate on the notebook <strong>"%s"</strong> with <strong>%s</strong> access.</p>
		%s
		<div style="text-align:center;margin:32px 0;">
			<a href="%s" style="background:#3b82f6;color:white;padding:12px 32px;text-decoration:none;border-radius:6px;font-size:16px;font-weight:600;display:inline-block;">Accept Invitation</a>
		</div>
		<p style="font-size:14px;color:#64748b;">Already have an account? <a href="%s" style="color:#3b82f6;">Sign in to accept</a></p>
		<p style="font-size:12px;color:#94a3b8;margin-top:24px;">This invitation expires in 7 days. If you didn't expect this email, you can safely ignore it.</p>
	</div>
</body>
</html>`, inviterName, notebookName, permission, personalMessage, signupURL, loginURL)

	return s.sendHTML(toEmail, subject, body)
}

// SendShareNotification notifies an existing user that a notebook was shared with them
func (s *EmailService) SendShareNotification(toEmail, inviterName, notebookName, permission string) error {
	if !s.config.Enabled {
		s.logger.Warn("SMTP disabled, skipping share notification",
			zap.String("to", toEmail),
		)
		return nil
	}

	subject := fmt.Sprintf("%s shared \"%s\" with you", inviterName, notebookName)
	notebookURL := fmt.Sprintf("%s/notebooks", s.config.AppBaseURL)

	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;max-width:600px;margin:0 auto;padding:20px;color:#1e293b;">
	<div style="text-align:center;padding:24px 0;border-bottom:1px solid #e2e8f0;">
		<h1 style="margin:0;color:#0f172a;font-size:24px;">Aether</h1>
	</div>
	<div style="padding:24px 0;">
		<p style="font-size:16px;"><strong>%s</strong> shared the notebook <strong>"%s"</strong> with you (%s access).</p>
		<div style="text-align:center;margin:32px 0;">
			<a href="%s" style="background:#3b82f6;color:white;padding:12px 32px;text-decoration:none;border-radius:6px;font-size:16px;font-weight:600;display:inline-block;">View Notebook</a>
		</div>
	</div>
</body>
</html>`, inviterName, notebookName, permission, notebookURL)

	return s.sendHTML(toEmail, subject, body)
}

func (s *EmailService) sendHTML(to, subject, htmlBody string) error {
	from := s.config.From
	if s.config.FromName != "" {
		from = fmt.Sprintf("%s <%s>", s.config.FromName, s.config.From)
	}

	headers := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
	}

	msg := []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + htmlBody)

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var auth smtp.Auth
	if s.config.User != "" {
		auth = smtp.PlainAuth("", s.config.User, s.config.Password, s.config.Host)
	}

	if err := smtp.SendMail(addr, auth, s.config.From, []string{to}, msg); err != nil {
		s.logger.Error("Failed to send email",
			zap.String("to", to),
			zap.String("subject", subject),
			zap.Error(err),
		)
		return fmt.Errorf("failed to send email: %w", err)
	}

	s.logger.Info("Email sent successfully",
		zap.String("to", to),
		zap.String("subject", subject),
	)
	return nil
}
