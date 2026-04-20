import { expect } from "@playwright/test";
import { BasePage } from "../base";

export class NavigationComponent extends BasePage {
  async clickNavigationMenuIfMobile() {
    if (this.isMobile) {
      await this.page.getByTestId("navigation-menu-button").click();
    }
  }

  async clickNavigationItem(itemName: string) {
    await this.page.getByTestId("navigation").getByRole("button", { name: itemName }).click();
  }

  async clickNavigationItemInSettings(itemName: string, expand: boolean) {
    if (expand) {
      await this.clickNavigationItem("Settings");
    }
    await this.clickNavigationItem(itemName);
  }

  async navigateToHome() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Home");
    await expect(this.page).toHaveURL(/.*\/hashrate/);
    await this.validateTitle("Home");
  }

  async navigateToDiagnostics() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Diagnostics");
    await expect(this.page).toHaveURL(/.*\/diagnostics/);
    await this.validateTitle("Diagnostics");
  }

  async navigateToLogs() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Logs");
    await expect(this.page).toHaveURL(/.*\/logs/);
  }

  async navigateToAuthenticationSettings(expand: boolean) {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItemInSettings("Authentication", expand);
    await expect(this.page).toHaveURL(/.*\/settings\/authentication/);
    await this.validateTitle("Update your admin login");
  }

  async navigateToGeneralSettings(expand: boolean) {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItemInSettings("General", expand);
    await expect(this.page).toHaveURL(/.*\/settings\/general/);
    await this.validateTitle("General");
  }

  async navigateToPoolsSettings(expand: boolean) {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItemInSettings("Pools", expand);
    await expect(this.page).toHaveURL(/.*\/settings\/mining-pools/);
  }

  async navigateToHardwareSettings(expand: boolean) {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItemInSettings("Hardware", expand);
    await expect(this.page).toHaveURL(/.*\/settings\/hardware/);
    await this.validateTitle("Hardware");
  }

  async navigateToCoolingSettings(expand: boolean) {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItemInSettings("Cooling", expand);
    await expect(this.page).toHaveURL(/.*\/settings\/cooling/);
    await this.validateTitle("Cooling");
  }
}
