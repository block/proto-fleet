import { test } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { HeaderComponent } from "../pages/components/header";
import { NavigationComponent } from "../pages/components/navigation";
import { SleepWakeDialogsComponent } from "../pages/components/sleepWakeDialog";
import { WakeCalloutComponent } from "../pages/components/wakeCallout";
import { WelcomePage } from "../pages/onboarding";

export class CommonSteps {
  constructor(
    private welcomePage: WelcomePage,
    private navigationComponent: NavigationComponent,
    private headerComponent: HeaderComponent,
    private sleepWakeDialogsComponent: SleepWakeDialogsComponent,
    private wakeCalloutComponent: WakeCalloutComponent,
  ) {}

  async authenticateAsAdmin() {
    await test.step("Authenticate as admin", async () => {
      await this.welcomePage.inputLoginPassword(testConfig.admin.password);
      await this.welcomePage.clickLoginButton();
      await this.welcomePage.validateToastMessage("You are now logged in as admin");
    });
  }

  async navigateToHome() {
    await test.step("Navigate to Home", async () => {
      await this.navigationComponent.navigateToHome();
    });
  }

  async navigateToDiagnostics() {
    await test.step("Navigate to Diagnostics", async () => {
      await this.navigationComponent.navigateToDiagnostics();
    });
  }

  async navigateToLogs() {
    await test.step("Navigate to Logs", async () => {
      await this.navigationComponent.navigateToLogs();
    });
  }

  async navigateToAuthenticationSettings(expand: boolean = true) {
    await test.step("Navigate to Authentication settings", async () => {
      await this.navigationComponent.navigateToAuthenticationSettings(expand);
    });
  }

  async navigateToGeneralSettings(expand: boolean = true) {
    await test.step("Navigate to General settings", async () => {
      await this.navigationComponent.navigateToGeneralSettings(expand);
    });
  }

  async navigateToPoolsSettings(expand: boolean = true) {
    await test.step("Navigate to Pools settings", async () => {
      await this.navigationComponent.navigateToPoolsSettings(expand);
    });
  }

  async navigateToHardwareSettings(expand: boolean = true) {
    await test.step("Navigate to Hardware settings", async () => {
      await this.navigationComponent.navigateToHardwareSettings(expand);
    });
  }

  async navigateToCoolingSettings(expand: boolean = true) {
    await test.step("Navigate to Cooling settings", async () => {
      await this.navigationComponent.navigateToCoolingSettings(expand);
    });
  }

  async validateWakeCallout() {
    await test.step(`Validate miner asleep status in current page`, async () => {
      await this.wakeCalloutComponent.validateWakeCallout();
    });
  }

  async putMinerToSleep() {
    await test.step(`Put miner to sleep from current page`, async () => {
      await this.headerComponent.clickPowerButton();
      await this.headerComponent.clickPowerPopoverButton("Sleep");
      await this.sleepWakeDialogsComponent.clickEnterSleepMode();
      await this.sleepWakeDialogsComponent.validateEnteringSleepDialog();
    });
  }

  async wakeMinerFromCallout() {
    await test.step(`Wake miner up from current page callout`, async () => {
      await this.wakeCalloutComponent.clickWakeMinerInCallout();
      await this.sleepWakeDialogsComponent.clickWakeMinerInDialog();
      await this.sleepWakeDialogsComponent.validateWakingDialog();
      await this.headerComponent.validateMinerStatus("Hashing");
      await this.wakeCalloutComponent.validateWakeCalloutNotVisible();
    });
  }
}
