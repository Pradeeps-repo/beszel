package migrations

import (
	"slices"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

var fleetAlertNames = []string{
	"Brightness",
	"BrightnessMissing",
	"Resolution",
	"Orientation",
	"SsApp",
	"SsIot",
	"Os",
}

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("name").(*core.SelectField)
		for _, name := range fleetAlertNames {
			if !slices.Contains(field.Values, name) {
				field.Values = append(field.Values, name)
			}
		}
		if collection.Fields.GetByName("expect") == nil {
			collection.Fields.Add(&core.TextField{
				Name: "expect",
				Max:  200,
			})
		}
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("name").(*core.SelectField)
		field.Values = slices.DeleteFunc(field.Values, func(name string) bool {
			return slices.Contains(fleetAlertNames, name)
		})
		collection.Fields.RemoveByName("expect")
		return app.Save(collection)
	})
}
