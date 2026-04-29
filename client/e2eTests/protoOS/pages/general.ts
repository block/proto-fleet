import { expect } from "@playwright/test";
import { BasePage } from "./base";

type ThemeLabel = "System" | "Light" | "Dark";
type ThemeColor = "light" | "dark";
type MinerIdState = "missing" | "existing";

export class GeneralPage extends BasePage {
  private async getMinerIdState(): Promise<MinerIdState> {
    const editButton = this.page.getByTestId("edit-details-button");

    try {
      await editButton.waitFor({ state: "visible", timeout: 3000 });
      return "existing";
    } catch {
      await this.page.getByTestId("add-miner-id").waitFor({ state: "visible" });
      return "missing";
    }
  }

  async clickThemeButton() {
    await this.page.getByTestId("theme-button").click();
  }

  async clickTemperatureButton() {
    await this.page.locator('[data-testid="temperature-button"]').click();
  }

  async selectTheme(theme: ThemeLabel) {
    await this.page.getByTestId(`theme-${theme.toLowerCase()}-option`).click();
  }

  async selectFahrenheit() {
    await this.page.locator('//*[@data-testid="fahrenheit-option"]//input').click();
  }

  async selectCelsius() {
    await this.page.locator('//*[@data-testid="celsius-option"]//input').click();
  }

  async clickDoneButton() {
    await this.clickButton("Done");
  }

  async getSelectedTheme(): Promise<ThemeLabel> {
    const theme = await this.page.getByTestId("theme-button").innerText();

    if (theme === "System" || theme === "Light" || theme === "Dark") {
      return theme;
    }

    throw new Error(`Unexpected theme value: ${theme}`);
  }

  async validateSelectedTheme(theme: ThemeLabel) {
    await expect(this.page.getByTestId("theme-button")).toHaveText(theme);
  }

  async validateBodyTheme(theme: ThemeColor) {
    await expect(this.page.locator("body")).toHaveAttribute("data-theme", theme);
  }

  private async validateTemperatureFormat(format: string) {
    await expect(this.page.locator('[data-testid="temperature-button"]')).toHaveText(format);
  }

  async validateTemperatureFormatFahrenheit() {
    await this.validateTemperatureFormat("Fahrenheit");
  }

  async validateTemperatureFormatCelsius() {
    await this.validateTemperatureFormat("Celsius");
  }

  async getMinerId(): Promise<string | null> {
    if ((await this.getMinerIdState()) === "missing") {
      return null;
    }

    return (await this.page.getByTestId("miner-id-value").innerText()).trim();
  }

  async openMinerIdEditor() {
    if ((await this.getMinerIdState()) === "existing") {
      await this.page.getByTestId("edit-details-button").click();
      return;
    }

    await this.page.getByTestId("add-miner-id").click();
  }

  async validateMinerIdModalOpened() {
    await this.validateModalIsOpen();
    await this.validateTitleInModal("Proto Rig identification");
  }

  async inputMinerId(value: string) {
    await this.page.getByTestId("miner-id-input").fill(value);
  }

  async saveMinerId() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Save" }).click();
  }

  async validateMinerIdSavedToast() {
    await expect(this.page.getByTestId("toast").getByText("Miner ID saved").last()).toBeVisible();
  }

  async validateMinerId(value: string) {
    await expect(this.page.getByTestId("miner-id-value")).toHaveText(value);
  }

  async restoreMinerIdIfNeeded(originalMinerId: string | null) {
    if (!originalMinerId) {
      return;
    }

    await this.openMinerIdEditor();
    await this.validateMinerIdModalOpened();
    await this.inputMinerId(originalMinerId);
    await this.saveMinerId();
    await this.validateMinerIdSavedToast();
    await this.validateMinerId(originalMinerId);
  }
}
