import { expect } from "@playwright/test";
import { BasePage } from "./base";

type ThemeLabel = "System" | "Light" | "Dark";
type ThemeColor = "light" | "dark";

// The miner's v1 system-tag endpoint. Test setup/teardown reads and clears the
// Miner ID here directly: the UI has no affordance to distinguish a not-yet-
// loaded tag from an unset one (needed for a reliable read), and none at all to
// clear a tag (needed to restore a rig that started without one).
const SYSTEM_TAG_PATH = "/api/v1/system/tag";

export class GeneralPage extends BasePage {
  // The app authenticates to the miner with a bearer it persists to
  // localStorage. Reuse that token for the suite's own requests rather than
  // depending on the app exposing its internal client on `window`.
  private async minerAuthHeaders(): Promise<Record<string, string>> {
    const token = await this.page.evaluate(() => {
      const raw = localStorage.getItem("proto-os-auth");
      if (!raw) return null;
      try {
        const parsed = JSON.parse(raw) as {
          state?: { auth?: { session?: { accessToken?: { value?: string } } } };
        };
        return parsed.state?.auth?.session?.accessToken?.value ?? null;
      } catch {
        return null;
      }
    });
    return token ? { Authorization: `Bearer ${token}` } : {};
  }

  private async getSystemTagFromApi(): Promise<string | null> {
    const response = await this.page.request.get(SYSTEM_TAG_PATH, { headers: await this.minerAuthHeaders() });
    if (response.status() === 404) {
      return null;
    }
    if (!response.ok()) {
      throw new Error(`Failed to read Miner ID: HTTP ${response.status()}`);
    }

    const data: unknown = await response.json();
    // Mirror the app's normalizeSystemTag (useSystemTag.ts): accept a raw string
    // or a { tag } object, and treat an empty/whitespace value as "no Miner ID
    // set". A { tag: "" } response (what the rig returns when unset) must read as
    // null so it matches the UI, which shows the "Add" affordance.
    let tag: string;
    if (typeof data === "string") {
      tag = data;
    } else if (data && typeof data === "object" && typeof (data as { tag?: unknown }).tag === "string") {
      tag = (data as { tag: string }).tag;
    } else {
      tag = JSON.stringify(data);
    }
    return tag.trim() || null;
  }

  private async deleteSystemTagViaApi() {
    const response = await this.page.request.delete(SYSTEM_TAG_PATH, { headers: await this.minerAuthHeaders() });
    if (!response.ok() && response.status() !== 404) {
      throw new Error(`Failed to reset Miner ID: HTTP ${response.status()}`);
    }
  }

  async clickThemeButton() {
    await this.page.getByTestId("theme-button").click();
  }

  async getFirmwareVersion() {
    const firmwareVersion = this.page.getByTestId("firmware-version-value");
    await expect.poll(async () => (await firmwareVersion.innerText()).trim()).not.toBe("");
    return (await firmwareVersion.innerText()).trim();
  }

  async validateFirmwareVersion(expected: string | RegExp) {
    await expect(this.page.getByTestId("firmware-version-value")).toHaveText(expected);
  }

  async validateCheckForUpdatesButtonVisible() {
    await expect(this.page.getByTestId("check-for-updates-button")).toBeVisible();
  }

  async clickCheckForUpdatesButton() {
    await this.page.getByTestId("check-for-updates-button").click();
  }

  async chooseFirmwareFile() {
    await this.page.getByRole("button", { name: "Upload firmware", exact: true }).click();
    const dialog = this.page.getByRole("dialog", { name: "Upload firmware", exact: true });
    await dialog.getByLabel("Firmware file").setInputFiles({
      name: "proto-os-test-update.swu",
      mimeType: "application/octet-stream",
      buffer: Buffer.from("fake firmware bundle for e2e"),
    });
    await expect(dialog.getByText("proto-os-test-update.swu", { exact: true })).toBeVisible();
  }

  async confirmFirmwareUpload() {
    await this.page
      .getByRole("dialog", { name: "Upload firmware", exact: true })
      .getByRole("button", { name: "Upload and install", exact: true })
      .click();
  }

  async validateFirmwareUploadError(message: RegExp) {
    await expect(this.page.getByRole("dialog", { name: "Upload firmware", exact: true }).getByRole("alert")).toHaveText(
      message,
    );
  }

  async validateInlineFirmwareStatus(expected: string | RegExp) {
    await expect(this.page.getByTestId("firmware-update-inline-status")).toHaveText(expected);
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
    const theme = (await this.page.getByTestId("theme-button").innerText()).trim();

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
    return this.getSystemTagFromApi();
  }

  async openMinerIdEditor() {
    if (await this.getSystemTagFromApi()) {
      await this.page.getByTestId("edit-details-button").waitFor({ state: "visible" });
      await this.page.getByTestId("edit-details-button").click();
      return;
    }

    await this.page.getByTestId("add-miner-id").waitFor({ state: "visible" });
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
    if (originalMinerId) {
      await this.openMinerIdEditor();
      await this.validateMinerIdModalOpened();
      await this.inputMinerId(originalMinerId);
      await this.saveMinerId();
      await this.validateMinerIdSavedToast();
      await this.validateMinerId(originalMinerId);
      return;
    }

    await this.deleteSystemTagViaApi();
    await this.reloadPage();
    await this.page.getByTestId("add-miner-id").waitFor({ state: "visible" });
  }
}
