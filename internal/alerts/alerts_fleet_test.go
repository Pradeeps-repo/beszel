//go:build testing

package alerts_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setBrightnessAlertValue(info *system.Info, stats *system.Stats, value float64) {
	if value < 0 {
		info.Brightness = nil
		stats.Brightness = nil
		return
	}
	reading := uint8(value)
	info.Brightness = &reading
	stats.Brightness = &reading
}

func TestBrightnessAlertBelowThreshold(t *testing.T) {
	fixture := newSystemAlertTestFixture(t, "Brightness", 1, 70)
	defer fixture.cleanup()

	synctest.Test(t, func(t *testing.T) {
		submitValue(fixture, t, float64(-1), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "A missing brightness reading must not fire")
		assert.Equal(t, 0, fixture.hub.TestMailer.TotalSend())

		submitValue(fixture, t, float64(45), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, true, "45% is below 70%")
		require.NotEmpty(t, fixture.hub.TestMailer.Messages())
		message := fixture.hub.TestMailer.Messages()[0]
		assert.Contains(t, message.Text, "Brightness on test-system-0 is 45% (below 70%)")

		submitValue(fixture, t, float64(80), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "80% recovers the alert")
		assert.Contains(t, fixture.hub.TestMailer.Messages()[1].Text, "at or above 70%")
	})
}

func TestBrightnessAlertAboveThreshold(t *testing.T) {
	fixture := newSystemAlertTestFixture(t, "Brightness", 1, 50)
	defer fixture.cleanup()
	alert, err := fixture.hub.FindRecordById("alerts", fixture.alertID)
	require.NoError(t, err)
	alert.Set("expect", "above")
	require.NoError(t, fixture.hub.Save(alert))

	synctest.Test(t, func(t *testing.T) {
		submitValue(fixture, t, float64(-1), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "A missing brightness reading must not fire")
		assert.Equal(t, 0, fixture.hub.TestMailer.TotalSend())

		submitValue(fixture, t, float64(40), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "40% is not above 50%")
		assert.Equal(t, 0, fixture.hub.TestMailer.TotalSend())

		submitValue(fixture, t, float64(80), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, true, "80% is above 50%")
		require.NotEmpty(t, fixture.hub.TestMailer.Messages())
		assert.Contains(t, fixture.hub.TestMailer.Messages()[0].Text, "Brightness on test-system-0 is 80% (above 50%)")

		submitValue(fixture, t, float64(50), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "50% recovers an above alert")
		assert.Contains(t, fixture.hub.TestMailer.Messages()[1].Text, "at or below 50%")
	})
}

func TestBrightnessMissingAlert(t *testing.T) {
	fixture := newSystemAlertTestFixture(t, "BrightnessMissing", 1, 0)
	defer fixture.cleanup()

	synctest.Test(t, func(t *testing.T) {
		submitValue(fixture, t, float64(80), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "A present reading is not missing")

		submitValue(fixture, t, float64(-1), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, true, "A missing reading fires")
		assert.Contains(t, fixture.hub.TestMailer.Messages()[0].Text, "Brightness reading on test-system-0 is missing.")

		submitValue(fixture, t, float64(70), setBrightnessAlertValue)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "A reading resolves the missing alert")
		assert.Contains(t, fixture.hub.TestMailer.Messages()[1].Text, "reporting again (70%)")
	})
}

func TestOrientationMismatchAndResolutionChange(t *testing.T) {
	fixture := newSystemAlertTestFixture(t, "Orientation", 1, 1)
	defer fixture.cleanup()
	alert, err := fixture.hub.FindRecordById("alerts", fixture.alertID)
	require.NoError(t, err)
	alert.Set("expect", "portrait")
	require.NoError(t, fixture.hub.Save(alert))

	setOrientation := func(info *system.Info, _ *system.Stats, value string) {
		info.Orientation = value
	}
	synctest.Test(t, func(t *testing.T) {
		submitValue(fixture, t, "landscape", setOrientation)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, true, "landscape does not match portrait")
		assert.Contains(t, fixture.hub.TestMailer.Messages()[0].Text, "Orientation on test-system-0 is landscape (expected portrait).")

		submitValue(fixture, t, "portrait", setOrientation)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "portrait matches the expected value")
	})

	change := newSystemAlertTestFixture(t, "Resolution", 1, 0)
	defer change.cleanup()
	setResolution := func(info *system.Info, _ *system.Stats, value string) {
		info.Screen = value
	}
	synctest.Test(t, func(t *testing.T) {
		submitValue(change, t, "1920x1080", setResolution)
		waitForSystemAlert(time.Second)
		change.assertTriggered(t, false, "The first resolution is the baseline")
		assert.Equal(t, 0, change.hub.TestMailer.TotalSend())

		submitValue(change, t, "1080x1920", setResolution)
		waitForSystemAlert(time.Second)
		change.assertTriggered(t, true, "A resolution change fires")
		assert.Contains(t, change.hub.TestMailer.Messages()[0].Text, "changed from 1920x1080 to 1080x1920")

		history, err := change.hub.FindFirstRecordByFilter("alerts_history", "alert_id={:id}", map[string]any{"id": change.alertID})
		require.NoError(t, err)
		assert.Equal(t, "Resolution", history.GetString("name"))
	})
}

func TestSsAppMissingAlert(t *testing.T) {
	fixture := newSystemAlertTestFixture(t, "SsApp", 1, 2)
	defer fixture.cleanup()

	setApp := func(info *system.Info, _ *system.Stats, value string) {
		info.SsApp = value
	}
	synctest.Test(t, func(t *testing.T) {
		submitValue(fixture, t, "1.2.3", setApp)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "A present ss-app version is not missing")

		submitValue(fixture, t, "", setApp)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, true, "A blank ss-app version is missing")
		assert.Contains(t, fixture.hub.TestMailer.Messages()[0].Text, "ss-app on test-system-0 is missing.")

		submitValue(fixture, t, "1.2.4", setApp)
		waitForSystemAlert(time.Second)
		fixture.assertTriggered(t, false, "A version resolves the missing alert")
		assert.Contains(t, fixture.hub.TestMailer.Messages()[1].Text, "ss-app on test-system-0 is now 1.2.4.")
	})
}
