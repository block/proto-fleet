import { expect, type Locator, type Page } from "@playwright/test";

type TopologyTarget = "Buildings" | "Groups" | "Racks" | "Sites";

const selectionModalTitles: Record<TopologyTarget, string> = {
  Buildings: "Select buildings",
  Groups: "Select groups",
  Racks: "Select racks",
  Sites: "Select sites",
};

export class CurtailmentModal {
  constructor(private readonly page: Page) {}

  async selectSite(siteName: string) {
    await this.selectTarget("Sites", siteName);
  }

  async selectBuilding(buildingName: string) {
    await this.selectTarget("Buildings", buildingName);
  }

  async selectRack(rackLabel: string) {
    await this.selectTarget("Racks", rackLabel);
  }

  async selectGroup(groupName: string) {
    await this.selectTarget("Groups", groupName);
  }

  async validateSelection(target: TopologyTarget, value: string) {
    await expect(this.targetButton(target)).toHaveAccessibleName(`${target} ${value}`);
  }

  async confirmRun() {
    const runButton = this.root.getByRole("button", { name: "Run curtailment", exact: true });
    if (await runButton.isVisible().catch(() => false)) {
      await runButton.click();
    } else {
      await this.root.getByTestId("overflow-menu-trigger").click();
      await this.page
        .getByTestId("modal-overflow-sheet-content")
        .getByRole("button", { name: "Run curtailment", exact: true })
        .click();
    }

    const forceConfirmation = this.page.getByTestId("curtailment-force-inclusion-confirmation");
    const runConfirmation = this.page.getByTestId("curtailment-run-confirmation");
    await expect(forceConfirmation.or(runConfirmation)).toBeVisible();
    if (await forceConfirmation.isVisible()) {
      await forceConfirmation.getByRole("button", { name: "Force include", exact: true }).click();
    }

    await expect(runConfirmation).toBeVisible();
    await runConfirmation.getByRole("button", { name: "Run curtailment", exact: true }).click();
  }

  private get root(): Locator {
    return this.page.getByTestId("full-screen-two-pane-modal");
  }

  private targetButton(target: TopologyTarget): Locator {
    return this.root.getByRole("button", { name: new RegExp(`^${target} `) });
  }

  private async selectTarget(target: TopologyTarget, optionLabel: string) {
    await this.targetButton(target).click();

    const modal = this.page.getByTestId("modal");
    await expect(modal.getByText(selectionModalTitles[target], { exact: true })).toBeVisible();
    const option = modal.getByText(optionLabel, { exact: true }).locator("xpath=ancestor::label[1]");
    await option.getByRole("checkbox").check();
    await modal.getByRole("button", { name: "Done", exact: true }).click();
    await expect(modal).toBeHidden();
  }
}
