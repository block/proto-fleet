import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsPage extends BasePage {
  async clickTemperatureButton() {
    await this.page.locator('[data-testid="temperature-button"]').click();
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
