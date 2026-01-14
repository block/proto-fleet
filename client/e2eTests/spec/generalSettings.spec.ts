/* eslint-disable playwright/expect-expect */
import { test } from "../fixtures/pageFixtures";

test.describe("General Settings", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Set temperature format", async ({ authPage, settingsPage, minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Navigate to general settings", async () => {
      await authPage.navigateToSettingsPage();
    });

    await test.step("Set temperature to Fahrenheit", async () => {
      await settingsPage.clickTemperatureButton();
      await settingsPage.selectFahrenheit();
      await settingsPage.clickDoneButton();
      await settingsPage.validateTemperatureFormatFahrenheit();
    });

    await commonSteps.goToMinersPage();

    await test.step("Verify miner temperature is displayed in Fahrenheit", async () => {
      // Workaround: proto rig miners don't have temperature displayed atm
      await minersPage.filterBitmainMiners();
      await minersPage.validateTemperatureUnitFahrenheit();
    });

    await test.step("Navigate back to settings", async () => {
      await authPage.navigateToSettingsPage();
    });

    await test.step("Change temperature format to Celsius", async () => {
      await settingsPage.clickTemperatureButton();
      await settingsPage.selectCelsius();
      await settingsPage.clickDoneButton();
      await settingsPage.validateTemperatureFormatCelsius();
    });

    await commonSteps.goToMinersPage();

    await test.step("Verify miner temperature is displayed in Celsius", async () => {
      // Workaround: proto rig miners don't have temperature displayed atm
      await minersPage.filterBitmainMiners();
      await minersPage.validateTemperatureUnitCelsius();
    });
  });
});
