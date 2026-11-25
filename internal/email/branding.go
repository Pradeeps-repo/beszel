package email

import (
	"regexp"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

const (
	// AppName is the product name used in {APP_NAME} email placeholders.
	AppName = "Bright.Watch"
	// SenderName is the From name when the saved sender is still a default.
	SenderName = "Bright.Watch Alerts"
)

var beszelWord = regexp.MustCompile(`(?i)beszel`)

// ApplyBranding sets the app name used by auth emails and the default sender
// name. A custom app name or sender name is left alone, and SMTP settings are
// not changed. Literal "Beszel" text in auth templates is replaced.
func ApplyBranding(app core.App) error {
	settings := app.Settings()
	changed := false
	if isDefaultAppName(settings.Meta.AppName) {
		settings.Meta.AppName = AppName
		changed = true
	}
	if isDefaultSenderName(settings.Meta.SenderName) {
		settings.Meta.SenderName = SenderName
		changed = true
	}
	if changed {
		if err := app.Save(settings); err != nil {
			return err
		}
	}
	collections, err := app.FindAllCollections()
	if err != nil {
		return err
	}
	for _, collection := range collections {
		if !collection.IsAuth() {
			continue
		}
		if !rebrandTemplates(collection) {
			continue
		}
		if err := app.Save(collection); err != nil {
			return err
		}
	}
	return nil
}

func isDefaultAppName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "beszel", "acme", "bright.blue":
		return true
	default:
		return false
	}
}

func isDefaultSenderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "support", "beszel", "acme", "bright.blue alerts":
		return true
	default:
		return false
	}
}

func rebrandTemplates(collection *core.Collection) bool {
	changed := false
	for _, tpl := range []*core.EmailTemplate{
		&collection.VerificationTemplate,
		&collection.ResetPasswordTemplate,
		&collection.ConfirmEmailChangeTemplate,
		&collection.OTP.EmailTemplate,
		&collection.AuthAlert.EmailTemplate,
	} {
		subject := replaceProductName(tpl.Subject)
		body := replaceProductName(tpl.Body)
		if subject == tpl.Subject && body == tpl.Body {
			continue
		}
		tpl.Subject = subject
		tpl.Body = body
		changed = true
	}
	return changed
}

func replaceProductName(value string) string {
	value = beszelWord.ReplaceAllString(value, AppName)
	// The previous default wrote Bright.Blue into any template that said Beszel.
	return strings.ReplaceAll(value, "Bright.Blue", AppName)
}
