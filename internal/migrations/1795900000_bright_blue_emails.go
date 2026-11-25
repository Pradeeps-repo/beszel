package migrations

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
	"github.com/henrygd/beszel/internal/email"
)

func init() {
	m.Register(func(app core.App) error {
		return email.ApplyBranding(app)
	}, nil)
}
