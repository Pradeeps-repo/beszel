package alerts

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

const alertNameBrightnessMissing = "BrightnessMissing"

// fleetTextAlerts compare a string reported on the system record.
var fleetTextAlerts = map[string]string{
	"Resolution":  "Resolution",
	"Orientation": "Orientation",
	"SsApp":       "ss-app",
	"SsIot":       "ss-iot",
	"Os":          "OS",
}

type fleetAlertState struct {
	Seen bool   `json:"seen,omitempty"`
	Last string `json:"last,omitempty"`
}

func isFleetTextAlert(name string) bool {
	_, ok := fleetTextAlerts[name]
	return ok
}

func isFleetFieldAlert(name string) bool {
	return name == alertNameBrightnessMissing || isFleetTextAlert(name)
}

// HandleFleetFieldAlerts evaluates brightness-missing and display/app string alerts.
// Brightness percent uses the numeric low-threshold path in HandleSystemAlerts.
func (am *AlertManager) HandleFleetFieldAlerts(systemRecord *core.Record, data *system.CombinedData) error {
	if data == nil {
		return nil
	}
	var result error
	names := make([]string, 0, len(fleetTextAlerts)+1)
	names = append(names, alertNameBrightnessMissing)
	for name := range fleetTextAlerts {
		names = append(names, name)
	}
	for _, name := range names {
		for _, alert := range am.alertsCache.GetAlertsByName(systemRecord.Id, name) {
			if err := am.evalFleetFieldAlert(systemRecord, data, alert); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}

func (am *AlertManager) evalFleetFieldAlert(systemRecord *core.Record, data *system.CombinedData, alert CachedAlertData) error {
	systemName := systemRecord.GetString("name")
	label, current := fleetFieldValue(alert.Name, data)
	mode := int(alert.Value)
	if alert.Name == alertNameBrightnessMissing {
		mode = 2
	}
	if mode == 0 && isFleetTextAlert(alert.Name) && !alert.FleetSeen {
		return am.saveFleetState(alert, fleetAlertState{Seen: true, Last: current})
	}
	active := fleetConditionActive(mode, current, alert)
	now := time.Now().UTC()
	min := max(1, int(alert.Min))
	if active {
		if alert.Triggered {
			return nil
		}
		if alert.PendingSince.IsZero() {
			pendingSince, err := am.setPendingContainerAlert(alert, now)
			if err != nil {
				return err
			}
			if pendingSince.IsZero() {
				return nil
			}
			alert.PendingSince = pendingSince
			if min > 1 {
				return nil
			}
		}
		if min > 1 && now.Before(alert.PendingSince.Add(time.Duration(min)*time.Minute)) {
			return nil
		}
		title, body := fleetTriggerMessage(systemName, label, mode, alert, current)
		var next *fleetAlertState
		if mode == 0 {
			next = &fleetAlertState{Seen: true, Last: current}
		}
		return am.finishFleetAlert(alert, systemRecord, true, title, body, next)
	}
	if err := am.clearPendingContainerAlert(alert); err != nil {
		return err
	}
	if !alert.Triggered {
		return nil
	}
	title, body := fleetResolveMessage(systemName, label, alert.Name, data, current)
	return am.finishFleetAlert(alert, systemRecord, false, title, body, nil)
}

func fleetFieldValue(name string, data *system.CombinedData) (label, current string) {
	switch name {
	case alertNameBrightnessMissing:
		if data.Info.Brightness == nil {
			return "Brightness", ""
		}
		return "Brightness", fmt.Sprintf("%d%%", *data.Info.Brightness)
	case "Resolution":
		return "Resolution", strings.TrimSpace(data.Info.Screen)
	case "Orientation":
		return "Orientation", strings.TrimSpace(data.Info.Orientation)
	case "SsApp":
		return "ss-app", strings.TrimSpace(data.Info.SsApp)
	case "SsIot":
		return "ss-iot", strings.TrimSpace(data.Info.SsIot)
	case "Os":
		return "OS", strings.TrimSpace(data.Info.OsRelease)
	default:
		return name, ""
	}
}

func fleetConditionActive(mode int, current string, alert CachedAlertData) bool {
	switch mode {
	case 2:
		return current == ""
	case 1:
		return current != strings.TrimSpace(alert.Expect)
	default:
		return alert.FleetSeen && current != alert.FleetLast
	}
}

func fleetTriggerMessage(systemName, label string, mode int, alert CachedAlertData, current string) (string, string) {
	shown := current
	if shown == "" {
		shown = "missing"
	}
	switch {
	case alert.Name == alertNameBrightnessMissing:
		msg := fmt.Sprintf("Brightness reading on %s is missing.", systemName)
		return msg, msg
	case mode == 2:
		msg := fmt.Sprintf("%s on %s is missing.", label, systemName)
		return msg, msg
	case mode == 1:
		msg := fmt.Sprintf("%s on %s is %s (expected %s).", label, systemName, shown, strings.TrimSpace(alert.Expect))
		return msg, msg
	default:
		from := alert.FleetLast
		if from == "" {
			from = "missing"
		}
		msg := fmt.Sprintf("%s on %s changed from %s to %s.", label, systemName, from, shown)
		return msg, msg
	}
}

func fleetResolveMessage(systemName, label, name string, data *system.CombinedData, current string) (string, string) {
	if name == alertNameBrightnessMissing && data.Info.Brightness != nil {
		msg := fmt.Sprintf("Brightness on %s is reporting again (%d%%).", systemName, *data.Info.Brightness)
		return msg, msg
	}
	shown := current
	if shown == "" {
		shown = "missing"
	}
	msg := fmt.Sprintf("%s on %s is now %s.", label, systemName, shown)
	return msg, msg
}

func (am *AlertManager) saveFleetState(alert CachedAlertData, state fleetAlertState) error {
	record, err := am.hub.FindRecordById("alerts", alert.Id)
	if err != nil {
		return err
	}
	record.Set("state", state)
	return am.hub.Save(record)
}

func (am *AlertManager) finishFleetAlert(alert CachedAlertData, systemRecord *core.Record, triggered bool, title, message string, next *fleetAlertState) error {
	record, err := am.hub.FindRecordById("alerts", alert.Id)
	if err != nil {
		return err
	}
	record.Set("triggered", triggered)
	record.Set("pending_since", nil)
	if next != nil {
		record.Set("state", next)
	}
	if err := am.hub.Save(record); err != nil {
		return err
	}
	return am.SendAlert(AlertMessageData{
		UserID:   alert.UserID,
		SystemID: systemRecord.Id,
		Title:    title,
		Message:  message,
		Link:     am.hub.MakeLink("system", systemRecord.Id),
		LinkText: "View " + systemRecord.GetString("name"),
	})
}
