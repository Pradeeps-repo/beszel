package alerts

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/henrygd/beszel/internal/hub/utils"
	"github.com/nicholas-fedor/shoutrrr"
)

// UpsertUserAlerts handles API request to create or update alerts for a user
// across multiple systems (POST /api/beszel/user-alerts)
func UpsertUserAlerts(e *core.RequestEvent) error {
	userID := e.Auth.Id

	reqData := struct {
		Min       uint8    `json:"min"`
		Value     float64  `json:"value"`
		Name      string   `json:"name"`
		Systems   []string `json:"systems"`
		Overwrite bool     `json:"overwrite"`
		Expect    string   `json:"expect"`
	}{}
	err := e.BindBody(&reqData)
	if err != nil || userID == "" || reqData.Name == "" || len(reqData.Systems) == 0 {
		return e.BadRequestError("Bad data", err)
	}

	if reqData.Name == "Brightness" {
		dir := strings.ToLower(strings.TrimSpace(reqData.Expect))
		if dir == "" {
			dir = "below"
		}
		if dir != "above" && dir != "below" {
			return e.BadRequestError("Brightness direction must be above or below", nil)
		}
		reqData.Expect = dir
	}

	if isFleetTextAlert(reqData.Name) {
		mode := int(reqData.Value)
		if mode < 0 || mode > 2 {
			return e.BadRequestError("Display alert mode must be 0 (change), 1 (mismatch), or 2 (missing)", nil)
		}
		reqData.Expect = strings.TrimSpace(reqData.Expect)
		if mode == 1 && reqData.Expect == "" {
			return e.BadRequestError("Expected value is required when the alert checks for a mismatch", nil)
		}
	}

	if isNetworkMonitorAlert(reqData.Name) {
		if msg := networkMonitorThresholdError(reqData.Name, reqData.Value); msg != "" {
			return e.BadRequestError(msg, nil)
		}
		reqData.Min = 0
	}

	alertsCollection, err := e.App.FindCachedCollectionByNameOrId("alerts")
	if err != nil {
		return err
	}

	err = e.App.RunInTransaction(func(txApp core.App) error {
		for _, systemId := range reqData.Systems {
			if !userHasSystem(txApp, userID, systemId) {
				continue
			}
			// find existing matching alert
			alertRecord, err := txApp.FindFirstRecordByFilter(alertsCollection,
				"system={:system} && name={:name} && user={:user}",
				dbx.Params{"system": systemId, "name": reqData.Name, "user": userID})

			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			// skip if alert already exists and overwrite is not set
			if !reqData.Overwrite && alertRecord != nil {
				continue
			}

			// create new alert if it doesn't exist
			if alertRecord == nil {
				alertRecord = core.NewRecord(alertsCollection)
				alertRecord.Set("user", userID)
				alertRecord.Set("system", systemId)
				alertRecord.Set("name", reqData.Name)
			}

			alertRecord.Set("value", reqData.Value)
			alertRecord.Set("min", reqData.Min)
			if isFleetTextAlert(reqData.Name) || reqData.Name == "Brightness" {
				alertRecord.Set("expect", strings.TrimSpace(reqData.Expect))
			}

			if err := txApp.SaveNoValidate(alertRecord); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, map[string]any{"success": true})
}

// DeleteUserAlerts handles API request to delete alerts for a user across multiple systems
// (DELETE /api/beszel/user-alerts)
func DeleteUserAlerts(e *core.RequestEvent) error {
	userID := e.Auth.Id

	reqData := struct {
		AlertName string   `json:"name"`
		Systems   []string `json:"systems"`
	}{}
	err := e.BindBody(&reqData)
	if err != nil || userID == "" || reqData.AlertName == "" || len(reqData.Systems) == 0 {
		return e.BadRequestError("Bad data", err)
	}

	var numDeleted uint16

	err = e.App.RunInTransaction(func(txApp core.App) error {
		for _, systemId := range reqData.Systems {
			if !userHasSystem(txApp, userID, systemId) {
				continue
			}
			// Find existing alert to delete
			alertRecord, err := txApp.FindFirstRecordByFilter("alerts",
				"system={:system} && name={:name} && user={:user}",
				dbx.Params{"system": systemId, "name": reqData.AlertName, "user": userID})

			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// alert doesn't exist, continue to next system
					continue
				}
				return err
			}

			if err := txApp.Delete(alertRecord); err != nil {
				return err
			}
			numDeleted++
		}
		return nil
	})

	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, map[string]any{"success": true, "count": numDeleted})
}

func userHasSystem(app core.App, userID, systemID string) bool {
	system, err := app.FindRecordById("systems", systemID)
	if err != nil {
		return false
	}
	shareAll, _ := utils.GetEnv("SHARE_ALL_SYSTEMS")
	return shareAll == "true" || slices.Contains(system.GetStringSlice("users"), userID)
}

// SendTestNotification handles API request to send a test notification to a specified Shoutrrr URL
func (am *AlertManager) SendTestNotification(e *core.RequestEvent) error {
	var data struct {
		URL string `json:"url"`
	}
	err := e.BindBody(&data)
	if err != nil || data.URL == "" {
		return e.BadRequestError("URL is required", err)
	}
	send := shoutrrr.Send
	if !e.Auth.IsSuperuser() && e.Auth.GetString("role") != "admin" {
		send = sendPublicNotification
	}
	err = am.sendShoutrrrAlert(data.URL, "Test Alert", "This is a notification from Bright.Watch.", am.hub.Settings().Meta.AppURL, "View Bright.Watch", send)
	if errors.Is(err, errInternalDestination) || errors.Is(err, errUnrestrictedService) {
		return e.ForbiddenError(err.Error(), nil)
	}
	if err != nil {
		return e.JSON(200, map[string]string{"err": err.Error()})
	}
	return e.JSON(200, map[string]bool{"err": false})
}
