import { BasePage } from "./base";

export class SettingsPage extends BasePage {
  async clickNavigateToTeamSettings() {
    await this.page.locator(`//*[@data-testid="secondary-nav"]//*[@href="/settings/team"]`).click();
  }

  async clickNavigateToMiningPoolsSettings() {
    await this.page.locator(`//*[@data-testid="secondary-nav"]//*[@href="/settings/mining-pools"]`).click();
  }
}
