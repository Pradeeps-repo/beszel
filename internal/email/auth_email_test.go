//go:build testing

package email_test

import (
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/mails"
	"github.com/henrygd/beszel/internal/email"
	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthEmailsUseBeszelLayout(t *testing.T) {
	hub, user := beszelTests.GetHubWithUser(t)
	defer hub.Cleanup()

	require.NoError(t, mails.SendRecordPasswordReset(hub, user))

	message := hub.TestMailer.LastMessage()
	assert.Contains(t, message.HTML, `class="card content"`, "should use the branded layout")
	assert.Contains(t, message.HTML, "<title>"+message.Subject+"</title>")
	assert.Contains(t, message.HTML, "/_/#/auth/confirm-password-reset/", "should keep the template content")
	assert.NotContains(t, message.HTML, "Source Sans Pro", "should drop the PocketBase layout")
	assertNoBeszel(t, message.Subject, message.HTML, message.From.Name)
}

func TestAuthEmailsHaveNoBeszel(t *testing.T) {
	hub, user := beszelTests.GetHubWithUser(t)
	defer hub.Cleanup()
	require.Equal(t, "Bright.Blue", hub.Settings().Meta.AppName)
	require.Equal(t, "Bright.Blue Alerts", hub.Settings().Meta.SenderName)

	require.NoError(t, mails.SendRecordVerification(hub, user))
	require.NoError(t, mails.SendRecordPasswordReset(hub, user))
	require.NoError(t, mails.SendRecordChangeEmail(hub, user, "new-address@example.com"))
	require.NoError(t, mails.SendRecordOTP(hub, user, "otp-id", "123456"))
	require.NoError(t, mails.SendRecordAuthAlert(hub, user, "203.0.113.5"))

	messages := hub.TestMailer.Messages()
	require.GreaterOrEqual(t, len(messages), 5)
	for _, message := range messages[len(messages)-5:] {
		assertNoBeszel(t, message.Subject, message.HTML, message.Text, message.From.Name)
		assert.Contains(t, message.HTML, "Bright.Blue")
	}
}

func TestApplyBrandingKeepsCustomSender(t *testing.T) {
	hub, _ := beszelTests.GetHubWithUser(t)
	defer hub.Cleanup()
	settings := hub.Settings()
	settings.Meta.AppName = "Fleet Desk"
	settings.Meta.SenderName = "Ops Desk"
	settings.Meta.SenderAddress = "ops@example.com"
	settings.SMTP.Host = "smtp.example.com"
	require.NoError(t, hub.Save(settings))

	require.NoError(t, email.ApplyBranding(hub))
	got := hub.Settings()
	assert.Equal(t, "Fleet Desk", got.Meta.AppName)
	assert.Equal(t, "Ops Desk", got.Meta.SenderName)
	assert.Equal(t, "ops@example.com", got.Meta.SenderAddress)
	assert.Equal(t, "smtp.example.com", got.SMTP.Host)
}

func assertNoBeszel(t *testing.T, parts ...string) {
	t.Helper()
	for _, part := range parts {
		assert.NotContains(t, strings.ToLower(part), "beszel")
	}
}
