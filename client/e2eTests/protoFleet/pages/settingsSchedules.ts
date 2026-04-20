import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";
import { ModalMinerSelectionList } from "./components/modalMinerSelectionList";

export class SettingsSchedulesPage extends BasePage {
  private readonly modalMinerList = new ModalMinerSelectionList(this.page.getByTestId("modal"));

  async validateSchedulesPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/schedules/);
    await this.validateTitle("Schedules");
    await this.validateButtonIsVisible("Add a schedule");
  }

  async clickAddSchedule() {
    await this.clickButton("Add a schedule");
    await this.validateTitle("Add a schedule");
  }

  async inputScheduleName(name: string) {
    await this.page.locator("#schedule-name").fill(name);
  }

  async selectActionType(label: string) {
    await this.selectOption("#schedule-action", "Action type", label);
  }

  async selectScheduleType(label: string) {
    await this.selectOption("#schedule-type", "Type", label);
  }

  async selectScheduleFrequency(label: string) {
    await this.selectOption("#schedule-frequency", "Frequency", label);
  }

  async validateSaveDisabled() {
    await expect(this.page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  }

  async validateSaveEnabled() {
    await expect(this.page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
  }

  async inputDayOfMonth(value: string) {
    const input = this.page.locator("#schedule-day-of-month");
    await input.fill(value);
    await input.blur();
  }

  async validateValidationMessage(text: string) {
    await expect(this.page.getByText(text, { exact: true })).toBeVisible();
  }

  async openWeekdaySelect() {
    await this.page.locator("#schedule-days-of-week").click();
  }

  async selectWeekday(label: string) {
    await this.openWeekdaySelect();
    await this.page.getByRole("option", { name: label, exact: true }).click();
    await this.page.locator("#schedule-days-of-week").click();
    await expect(this.page.getByRole("listbox", { name: "Days options" })).toBeHidden();
  }

  async selectStartDate(daysFromToday: number) {
    const today = new Date();
    const target = new Date();
    target.setDate(target.getDate() + daysFromToday);
    const monthDelta = (target.getFullYear() - today.getFullYear()) * 12 + (target.getMonth() - today.getMonth());

    await this.page.getByTestId("schedule-start-date-trigger").click();
    await expect(this.page.getByTestId("schedule-start-date-calendar")).toBeVisible();

    for (let i = 0; i < Math.max(monthDelta, 0); i += 1) {
      await this.page.getByTestId("schedule-start-date-calendar-next-month").click();
    }

    for (let i = 0; i < Math.max(-monthDelta, 0); i += 1) {
      await this.page.getByTestId("schedule-start-date-calendar-prev-month").click();
    }

    await this.page.getByTestId(`schedule-start-date-calendar-day-${target.getDate()}`).click();
  }

  async openMinersTargetSelector() {
    await this.page
      .locator("button")
      .filter({ has: this.page.getByText("Miners", { exact: true }) })
      .first()
      .click();
    await this.validateTitleInModal("Select miners");
  }

  async waitForMinerSelectionModalToLoad() {
    await this.modalMinerList.waitForListToLoad();
  }

  async selectFirstMiners(count: number) {
    const indexes = await this.modalMinerList.getSelectableRowIndexes(count);
    if (indexes.length < count) {
      throw new Error(`Expected at least ${count} selectable miners, found ${indexes.length}`);
    }

    await this.modalMinerList.selectRowsByIndex(indexes);
  }

  async confirmMinerSelection() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  async clickSaveSchedule() {
    await this.clickButton("Save");
  }

  async validateScheduleVisible(name: string) {
    await expect(this.getScheduleRow(name)).toBeVisible();
  }

  async validateScheduleNotVisible(name: string) {
    await expect(this.getScheduleRows(name)).toHaveCount(0);
  }

  async validateScheduleStatus(name: string, expectedStatus: string) {
    await expect(this.getScheduleRow(name).getByTestId("status")).toContainText(expectedStatus);
  }

  async validateScheduleAction(name: string, expectedAction: string) {
    await expect(this.getScheduleRow(name).getByTestId("action").first()).toHaveText(expectedAction);
  }

  async validateScheduleSummary(name: string, expectedSummary: string) {
    await expect(this.getScheduleRow(name).getByTestId("schedule")).toContainText(expectedSummary);
  }

  async validateScheduleTargetSummary(name: string, expectedSummary: string) {
    await expect(this.getScheduleRow(name).getByTestId("name")).toContainText(expectedSummary);
  }

  async openScheduleActions(name: string) {
    const row = this.getScheduleRow(name);
    await expect(row).toBeVisible();
    await row.getByTestId("list-actions-trigger").click();
  }

  async clickScheduleAction(actionName: string) {
    await this.page.getByText(actionName, { exact: true }).click();
  }

  async openEditSchedule(name: string) {
    await this.openScheduleActions(name);
    await this.clickScheduleAction("Edit");
    await this.validateTitle("Edit schedule");
  }

  async pauseSchedule(name: string) {
    await this.openScheduleActions(name);
    await this.clickScheduleAction("Pause");
  }

  async resumeSchedule(name: string) {
    await this.openScheduleActions(name);
    await this.clickScheduleAction("Resume");
  }

  async deleteSchedule(name: string) {
    await this.openScheduleActions(name);
    await this.clickScheduleAction("Delete");
    await this.validateScheduleNotVisible(name);
  }

  async waitForSchedulesListToLoad() {
    const rows = this.page.getByTestId("list-row");
    const emptyState = this.page.getByText("Configure schedules to automate actions for your miners.");

    await expect(this.page.getByRole("button", { name: "Add a schedule" })).toBeVisible();

    if (await emptyState.isVisible().catch(() => false)) {
      return;
    }

    await expect(async () => {
      const rowCount = await rows.count();
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
      const rowCountAfterDelay = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async deleteSchedulesByPrefix(prefix: string) {
    await this.waitForSchedulesListToLoad();

    const rows = await this.page.getByTestId("list-row").all();
    const scheduleNames: string[] = [];

    for (const row of rows) {
      const name = (await row.getByTestId("name").locator("span").first().textContent())?.trim();
      if (name?.startsWith(prefix)) {
        scheduleNames.push(name);
      }
    }

    for (const scheduleName of scheduleNames) {
      await this.deleteSchedule(scheduleName);
    }
  }

  private getScheduleRow(name: string) {
    return this.getScheduleRows(name).first();
  }

  private getScheduleRows(name: string) {
    return this.page.getByTestId("list-row").filter({
      has: this.page.getByTestId("name").getByText(name, { exact: true }),
    });
  }

  private async selectOption(triggerSelector: string, label: string, optionLabel: string) {
    await this.page.locator(triggerSelector).click();
    await this.page.getByRole("option", { name: optionLabel, exact: true }).click();
    await expect(this.page.getByRole("button", { name: label, exact: true })).toContainText(optionLabel);
  }
}
