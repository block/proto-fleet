import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class HardwarePage extends BasePage {
  private controlBoardSection() {
    return this.page.getByTestId("hardware-control-board-section");
  }

  private hashboardsSection() {
    return this.page.getByTestId("hardware-hashboards-section");
  }

  private fansSection() {
    return this.page.getByTestId("hardware-fans-section");
  }

  private psusSection() {
    return this.page.getByTestId("hardware-psus-section");
  }

  async validateSectionHeadings() {
    await expect(this.controlBoardSection().getByRole("heading", { name: "Control Board" })).toBeVisible();
    await expect(this.hashboardsSection().getByRole("heading", { name: "Hashboards" })).toBeVisible();
    await expect(this.fansSection().getByRole("heading", { name: "Fans" })).toBeVisible();
    await expect(this.psusSection().getByRole("heading", { name: "Power supply" })).toBeVisible();
  }

  async validateControlBoardSerialLooksLikeSimulatorData() {
    await expect(this.controlBoardSection().getByText(/PROTO-SIM-/)).toBeVisible();
  }

  async validateHashboardInventory() {
    await expect(this.hashboardsSection().getByText("Model B4_128")).toHaveCount(4);
    await expect(this.hashboardsSection().getByText(/HB-PROTO-SIM-/)).toHaveCount(4);
  }

  async validateFanInventory() {
    await expect(this.fansSection().getByText("Fan 1")).toBeVisible();
    await expect(this.fansSection().getByText("Fan 2")).toBeVisible();
    await expect(this.fansSection().getByText("Fan 3")).toBeVisible();
    await expect(this.fansSection().getByText("Fan 4")).toBeVisible();
  }

  async validatePsuInventory() {
    await expect(this.psusSection().getByText("Model PSU-3600W")).toHaveCount(2);
    await expect(this.psusSection().getByText(/PSU-PROTO-SIM-/)).toHaveCount(2);
  }

  async validateNoFansConnectedCalloutHidden() {
    await expect(this.fansSection().getByText("No fans connected")).toHaveCount(0);
    await expect(this.fansSection().getByText("This miner is set to immersion cooling")).toHaveCount(0);
  }
}
