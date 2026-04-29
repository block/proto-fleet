import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

function getThemeSwitchTarget(currentTheme: "System" | "Light" | "Dark") {
  return currentTheme === "Dark" ? "Light" : "Dark";
}

function getThemeColor(theme: "Light" | "Dark") {
  return theme === "Dark" ? "dark" : "light";
}

test.describe("General settings", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
  });

  test("Theme preference persists after reload", async ({ generalPage, commonSteps }) => {
    await commonSteps.navigateToGeneralSettings();

    const originalTheme = await generalPage.getSelectedTheme();
    const nextTheme = getThemeSwitchTarget(originalTheme);

    await test.step("Switch to a different explicit theme", async () => {
      await generalPage.clickThemeButton();
      await generalPage.selectTheme(nextTheme);
      await generalPage.clickDoneButton();
      await generalPage.validateSelectedTheme(nextTheme);
      await generalPage.validateBodyTheme(getThemeColor(nextTheme));
    });

    await test.step("Reload and validate the theme persists", async () => {
      await generalPage.reloadPage();
      await generalPage.validateSelectedTheme(nextTheme);
      await generalPage.validateBodyTheme(getThemeColor(nextTheme));
    });

    await test.step("Restore the original theme preference", async () => {
      await generalPage.clickThemeButton();
      await generalPage.selectTheme(originalTheme);
      await generalPage.clickDoneButton();
      await generalPage.validateSelectedTheme(originalTheme);
    });
  });

  test("Miner ID can be added or edited", async ({ generalPage, commonSteps }) => {
    const nextMinerId = generateRandomText("miner-id");
    await commonSteps.navigateToGeneralSettings();

    const originalMinerId = await generalPage.getMinerId();

    await test.step("Save a new Miner ID", async () => {
      await generalPage.openMinerIdEditor();
      await generalPage.validateMinerIdModalOpened();
      await generalPage.inputMinerId(nextMinerId);
      await generalPage.saveMinerId();
      await generalPage.validateMinerIdSavedToast();
      await generalPage.validateMinerId(nextMinerId);
    });

    await test.step("Restore the original Miner ID when one already existed", async () => {
      await generalPage.restoreMinerIdIfNeeded(originalMinerId);
    });
  });
});
