/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("General Settings", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Set temperature format", async ({ authPage, settingsPage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to general settings", async () => {
      await authPage.navigateToSettingsPage();
    });

    await test.step("Set temperature to Fahrenheit", async () => {
      await settingsPage.clickTemperatureButton();
      await settingsPage.selectFahrenheit();
      await settingsPage.clickDoneButton();
      await settingsPage.validateTemperatureFormatFahrenheit();
    });

    await test.step("Navigate to miners page and verify Fahrenheit", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.validateMinersPageOpened();
      await minersPage.validateTemperatureUnitFahrenheit();
    });

    await test.step("Navigate back to settings and change to Celsius", async () => {
      await authPage.navigateToSettingsPage();
      await settingsPage.clickTemperatureButton();
      await settingsPage.selectCelsius();
      await settingsPage.clickDoneButton();
      await settingsPage.validateTemperatureFormatCelsius();
    });

    await test.step("Navigate to miners page and verify Celsius", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.validateMinersPageOpened();
      await minersPage.validateTemperatureUnitCelsius();
    });
  });
});
