import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsPage, type SettingsTheme } from "../pages/settings";

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

  test("Render network details from the fleet network info API", async ({
    authPage,
    settingsPage,
    commonSteps,
    page,
  }) => {
    await commonSteps.loginAsAdmin();

    const networkInfoResponsePromise = page.waitForResponse((response) => response.url().includes("GetNetworkInfo"));

    let subnet = "";
    let gateway = "";

    await test.step("Navigate to general settings and capture the network info response", async () => {
      await authPage.navigateToSettingsPage();
      const response = await networkInfoResponsePromise;
      const body = await response.json();

      subnet = body.networkInfo?.subnet ?? "";
      gateway = body.networkInfo?.gateway ?? "";
    });

    await test.step("Validate network details are rendered", async () => {
      test.expect(subnet).toBeTruthy();
      test.expect(gateway).toBeTruthy();
      await settingsPage.validateNetworkDetails(subnet, gateway);
    });
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

  test("Theme preference persists after refresh", async ({ authPage, settingsPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    let originalTheme: SettingsTheme = "System";
    let targetTheme: "Light" | "Dark" = "Dark";

    const targetThemeByCurrentTheme: Record<SettingsTheme, "Light" | "Dark"> = {
      Dark: "Light",
      Light: "Dark",
      System: "Dark",
    };
    const bodyThemeByTheme: Record<"Light" | "Dark", "light" | "dark"> = {
      Light: "light",
      Dark: "dark",
    };

    await test.step("Navigate to general settings and capture the current theme", async () => {
      await authPage.navigateToSettingsPage();
      originalTheme = await settingsPage.getCurrentTheme();
      targetTheme = targetThemeByCurrentTheme[originalTheme] ?? "Dark";
    });

    await test.step("Change the theme to a deterministic value", async () => {
      await settingsPage.clickThemeButton();
      await settingsPage.selectTheme(targetTheme);
      await settingsPage.clickDoneButton();
      await settingsPage.validateCurrentTheme(targetTheme);
      await settingsPage.validateBodyTheme(bodyThemeByTheme[targetTheme]);
    });

    await test.step("Refresh and validate theme persistence", async () => {
      await settingsPage.reloadPage();
      await settingsPage.validateCurrentTheme(targetTheme);
      await settingsPage.validateBodyTheme(bodyThemeByTheme[targetTheme]);
    });

    await test.step("Restore the original theme", async () => {
      await settingsPage.clickThemeButton();
      await settingsPage.selectTheme(originalTheme);
      await settingsPage.clickDoneButton();
      await settingsPage.validateCurrentTheme(originalTheme);
    });
  });
});
