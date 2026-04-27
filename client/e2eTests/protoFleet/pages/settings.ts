import { expect } from "@playwright/test";
import { BasePage } from "./base";

export type SettingsTheme = "System" | "Light" | "Dark";

export class SettingsPage extends BasePage {
  async clickTemperatureButton() {
    await this.page.locator('[data-testid="temperature-button"]').click();
  }

  async clickThemeButton() {
    const currentTheme = await this.getCurrentTheme();
    await this.page.getByRole("button", { name: currentTheme, exact: true }).click();
    await this.validateTitleInModal("Theme");
  }

  async selectTheme(theme: SettingsTheme) {
    await this.page.getByTestId("modal").getByText(theme, { exact: true }).click();
  }

  async getCurrentTheme(): Promise<SettingsTheme> {
    const themeButton = this.page
      .getByRole("button", {
        name: /^(System|Light|Dark)$/,
      })
      .first();
    const currentTheme = (await themeButton.innerText()).trim();

    if (currentTheme === "System" || currentTheme === "Light" || currentTheme === "Dark") {
      return currentTheme;
    }

    throw new Error(`Unexpected theme value: ${currentTheme}`);
  }

  async validateCurrentTheme(theme: SettingsTheme) {
    await expect(this.page.getByRole("button", { name: theme, exact: true }).first()).toBeVisible();
  }

  async validateBodyTheme(theme: "light" | "dark") {
    await expect(this.page.locator("body")).toHaveAttribute("data-theme", theme);
  }

  async validateNetworkDetails(subnet: string, gateway: string) {
    await expect(this.page.getByText(subnet, { exact: true })).toBeVisible();
    await expect(this.page.getByText(gateway, { exact: true })).toBeVisible();
  }

  async selectFahrenheit() {
    await this.page.getByTestId("fahrenheit-option").click();
  }

  async selectCelsius() {
    await this.page.getByTestId("celsius-option").click();
  }

  async clickDoneButton() {
    await this.clickButton("Done");
  }

  async getCurrentTemperatureFormat(): Promise<string> {
    return await this.page.locator('[data-testid="temperature-button"]').innerText();
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
}
