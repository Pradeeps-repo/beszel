// Package email renders Bright.Watch HTML emails.
package email

import (
	_ "embed"
	"html/template"
	"regexp"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

var (
	//go:embed layout.html
	layoutSource string
	//go:embed alert.html
	alertSource string
)

var (
	layoutTemplate  = template.Must(template.New("layout").Parse(layoutSource))
	alertTemplate   = template.Must(template.Must(layoutTemplate.Clone()).Parse(alertSource))
	contentTemplate = template.Must(template.Must(layoutTemplate.Clone()).Parse(`{{define "content"}}{{.Content}}{{end}}`))
)

// Alert is the content of an alert email.
type Alert struct {
	Title        string
	Message      string
	Link         string
	LinkText     string
	SettingsLink string
	// AppURL is the hub's public URL. The logo is loaded from it.
	AppURL string
}

type mailPage struct {
	Title   string
	LogoURL string
	Content template.HTML
	Alert
	Blocks []block
}

// block is a paragraph or a fenced code block of an alert message.
type block struct {
	Code  bool
	Lines []string
}

// RenderAlert renders the HTML body of an alert email.
func RenderAlert(a Alert) (string, error) {
	if a.LinkText == "" {
		a.LinkText = "Open Bright.Watch"
	}
	var sb strings.Builder
	err := alertTemplate.Execute(&sb, mailPage{
		Title:   a.Title,
		LogoURL: LogoURL(a.AppURL),
		Alert:   a,
		Blocks:  messageBlocks(a.Message),
	})
	return sb.String(), err
}

// messageBlocks splits a message into code blocks (``` fences) and
// paragraphs (separated by blank lines).
func messageBlocks(message string) []block {
	var blocks []block
	var lines []string
	inCode := false
	flush := func() {
		if len(lines) > 0 {
			blocks = append(blocks, block{Code: inCode, Lines: lines})
		}
		lines = nil
	}
	for line := range strings.SplitSeq(strings.ReplaceAll(message, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(strings.TrimSpace(line), "```"):
			flush()
			inCode = !inCode
		case !inCode && strings.TrimSpace(line) == "":
			flush()
		default:
			lines = append(lines, line)
		}
	}
	flush()
	return blocks
}

// BindAuthEmails restyles PocketBase's auth emails (verification, password
// reset, email change, OTP and login alert) with the Bright.Watch layout.
func BindAuthEmails(app core.App) {
	app.OnMailerRecordVerificationSend().BindFunc(restyleAuthEmail)
	app.OnMailerRecordPasswordResetSend().BindFunc(restyleAuthEmail)
	app.OnMailerRecordEmailChangeSend().BindFunc(restyleAuthEmail)
	app.OnMailerRecordOTPSend().BindFunc(restyleAuthEmail)
	app.OnMailerRecordAuthAlertSend().BindFunc(restyleAuthEmail)
}

func restyleAuthEmail(e *core.MailerRecordEvent) error {
	if html, ok := wrapPocketBaseHTML(e.Message.Subject, e.Message.HTML, e.App.Settings().Meta.AppURL); ok {
		e.Message.HTML = html
	} else {
		e.App.Logger().Warn("Could not restyle auth email, sending PocketBase layout", "subject", e.Message.Subject)
	}
	return e.Next()
}

// wrapPocketBaseHTML replaces PocketBase's mail layout with the Bright.Watch layout,
// keeping the (possibly admin-customized) template content inside <body>.
func wrapPocketBaseHTML(title, pbHTML, appURL string) (string, bool) {
	start := strings.Index(pbHTML, "<body>")
	end := strings.LastIndex(pbHTML, "</body>")
	if start == -1 || end < start {
		return "", false
	}
	// The content comes from the collection's email template, which PocketBase
	// already renders as trusted HTML with escaped placeholder values.
	content := template.HTML(inlineBrandButton(strings.TrimSpace(pbHTML[start+len("<body>") : end])))
	var sb strings.Builder
	if err := contentTemplate.Execute(&sb, mailPage{
		Title:   title,
		LogoURL: LogoURL(appURL),
		Content: content,
	}); err != nil {
		return "", false
	}
	return sb.String(), true
}

// LogoURL is the absolute PNG address email clients can load.
func LogoURL(appURL string) string {
	base := strings.TrimRight(strings.TrimSpace(appURL), "/")
	if base == "" || strings.Contains(base, " ") {
		return ""
	}
	return base + "/static/icon.png"
}

var brandButton = regexp.MustCompile(`(?is)<a\s+class="btn"\s+href="([^"]+)"[^>]*>(.*?)</a>`)

// inlineBrandButton puts the brand colour on the template's primary link so
// clients that drop <style> (Outlook, Gmail) still show the button.
func inlineBrandButton(html string) string {
	return brandButton.ReplaceAllString(html, `<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:8px 0;"><tr><td bgcolor="#183EF6" style="background-color:#183EF6;border-radius:6px;"><a href="$1" style="display:inline-block;padding:12px 22px;font-family:Arial,Helvetica,sans-serif;font-size:15px;font-weight:700;color:#ffffff;text-decoration:none;">$2</a></td></tr></table>`)
}
