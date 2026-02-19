import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class GeneralPage extends BasePage {
  async clickTemperatureButton() {
    await this.page.click('[data-testid="temperature-button"]');
  }

  async selectFahrenheit() {
    await this.page.click('//*[@data-testid="fahrenheit-option"]//input');
  }

  async selectCelsius() {
    await this.page.click('//*[@data-testid="celsius-option"]//input');
  }

  async clickDoneButton() {
    await this.click("Done");
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
