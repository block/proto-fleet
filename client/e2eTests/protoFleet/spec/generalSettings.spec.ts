/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsPage } from "../pages/settings";

test.describe("General Settings", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterAll("CLEANUP: Ensure temperature is Celsius", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });
    const page = await context.newPage();
    await page.goto("/");

    try {
      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const settingsPage = new SettingsPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await authPage.navigateToSettingsPage();

      const currentTemperature = await settingsPage.getCurrentTemperatureFormat();

      if (currentTemperature !== "Celsius") {
        await settingsPage.clickTemperatureButton();
        await settingsPage.selectCelsius();
        await settingsPage.clickDoneButton();
        await settingsPage.validateTemperatureFormatCelsius();
      }
    } finally {
      await context.close();
    }
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
      await minersPage.validateTemperatureUnitCelsius();
    });
  });
});
