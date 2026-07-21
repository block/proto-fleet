import { expect, type Locator, type Request, type Response } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

const stopRequestPattern = /StopCurtailment/;
const restoreReconciliationTimeout = DEFAULT_TIMEOUT * 4;
const escapeRegex = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

export interface CurtailmentCleanupTarget {
  reason: string;
  eventUuid?: string;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export class EnergyPage extends BasePage {
  async navigateToEnergyPage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/energy"]').click();
    await expect(this.page).toHaveURL(/.*\/energy/);
  }

  async validateEnergyPageOpened() {
    await this.validateTitle("Curtailment");
    await this.validateTitle("Curtailment history");
  }

  async validateRunCurtailmentButtonHidden() {
    await expect(this.page.getByRole("button", { name: "Run curtailment", exact: true })).toHaveCount(0);
  }

  async openCurtailmentPlanner() {
    await this.clickButton("Run curtailment");
    await expect(this.page.getByTestId("full-screen-two-pane-modal").getByText("New curtailment")).toBeVisible();
  }

  async fillCurtailmentPlan({
    reason,
    targetKw,
    restoreBatchIntervalSec,
  }: {
    reason: string;
    targetKw: string;
    restoreBatchIntervalSec: string;
  }) {
    const modal = this.page.getByTestId("full-screen-two-pane-modal");

    await modal.locator("#curtailment-reason").fill(reason);
    // Full shutdown is the default mode; switch to fixed-kW so the target
    // input renders.
    await modal.locator("#curtailment-mode").click();
    await this.page.getByRole("option", { name: "Fixed kW reduction", exact: true }).click();
    await modal.locator("#curtailment-target-kw").fill(targetKw);
    await modal.locator("#curtailment-restore-batch-interval").fill(restoreBatchIntervalSec);
  }

  async waitForPreview(targetKw: string) {
    const modal = this.page.getByTestId("full-screen-two-pane-modal");
    const targetInput = modal.locator("#curtailment-target-kw");
    const startButton = modal.getByRole("button", { name: "Run curtailment" });
    const deadline = Date.now() + DEFAULT_TIMEOUT * 2;

    do {
      const previewResponse = this.page
        .waitForResponse(/PreviewCurtailmentPlan/, { timeout: DEFAULT_INTERVAL * 4 })
        .catch(() => undefined);
      await targetInput.fill("");
      await targetInput.fill(targetKw);
      const response = await previewResponse;
      if (response?.status() === 200) {
        await delay(DEFAULT_INTERVAL);
        if (await startButton.isEnabled().catch(() => false)) {
          return;
        }
      }

      await delay(DEFAULT_INTERVAL);
    } while (Date.now() < deadline);

    expect(false, "Curtailment preview did not become ready").toBe(true);
  }

  async startCurtailment() {
    await this.page.getByTestId("full-screen-two-pane-modal").getByRole("button", { name: "Run curtailment" }).click();
    const forceInclusionConfirmation = this.page.getByTestId("curtailment-force-inclusion-confirmation");
    const runConfirmation = this.page.getByTestId("curtailment-run-confirmation");

    await expect(forceInclusionConfirmation.or(runConfirmation)).toBeVisible();
    if (await forceInclusionConfirmation.isVisible()) {
      await forceInclusionConfirmation.getByRole("button", { name: "Force include" }).click();
    }
    await expect(runConfirmation).toBeVisible();
    await runConfirmation.getByRole("button", { name: "Run curtailment" }).click();
    await expect(this.page.getByTestId("full-screen-two-pane-modal")).toBeHidden();
  }

  async validateActiveCurtailment(reason: string) {
    await this.validateTitle("Active curtailment");
    const activeCurtailmentSection = this.activeCurtailmentSection(reason);

    await expect(activeCurtailmentSection).toBeVisible();
    await expect(activeCurtailmentSection).toContainText(/Dispatch status\s*(Pending|Curtailing|Curtailed)/);
  }

  async validateActiveCurtailmentManageActionsHidden(reason: string) {
    const activeCurtailmentSection = this.activeCurtailmentSection(reason);
    await expect(activeCurtailmentSection).toBeVisible();
    await expect(activeCurtailmentSection.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
    await expect(activeCurtailmentSection.getByRole("button", { name: "Stop", exact: true })).toHaveCount(0);
    await expect(activeCurtailmentSection.getByRole("button", { name: "Restore", exact: true })).toHaveCount(0);
    await expect(activeCurtailmentSection.getByRole("button", { name: "Restore now", exact: true })).toHaveCount(0);
  }

  async validateCurtailmentHistoryRow(reason: string) {
    await expect(this.curtailmentHistoryRowByReason(reason)).toBeVisible();
  }

  async stopMatchingActiveCurtailmentIfPresent(reason: string): Promise<boolean> {
    const stopButton = this.activeCurtailmentStopButton(reason);
    if (await stopButton.isVisible().catch(() => false)) {
      await this.confirmStopAction(stopButton, "Confirm stop");
      return true;
    }

    const restoreButton = this.activeCurtailmentRestoreButton(reason);
    if (await restoreButton.isVisible().catch(() => false)) {
      await this.confirmStopAction(restoreButton, "Restore power");
      return true;
    }

    return false;
  }

  async stopCurtailment(target: CurtailmentCleanupTarget) {
    await expect
      .poll(async () => this.hasMatchingStoppableCurtailment(target), {
        timeout: DEFAULT_TIMEOUT,
        intervals: [DEFAULT_INTERVAL],
      })
      .toBe(true);

    if (await this.stopMatchingActiveCurtailmentIfPresent(target.reason)) {
      return;
    }

    await this.stopHistoryCurtailmentIfPresent(target);
  }

  async waitForCurtailmentToRestore(target: CurtailmentCleanupTarget) {
    const activeCurtailmentSection = this.activeCurtailmentSection(target.reason);
    const terminalRestoreState = "terminal";
    const inactiveState = "inactive";

    await expect
      .poll(
        async () => {
          if (!(await activeCurtailmentSection.isVisible().catch(() => false))) {
            return inactiveState;
          }

          const sectionText = (await activeCurtailmentSection.textContent()) ?? "";
          return /Dispatch status\s*(Restored|Restore incomplete)/.test(sectionText)
            ? terminalRestoreState
            : sectionText;
        },
        { timeout: restoreReconciliationTimeout, intervals: [DEFAULT_INTERVAL] },
      )
      .toMatch(new RegExp(`${terminalRestoreState}|${inactiveState}`));

    const dismissButton = activeCurtailmentSection.getByRole("button", { name: "Dismiss", exact: true });
    if (await dismissButton.isVisible().catch(() => false)) {
      await dismissButton.click();
      await activeCurtailmentSection.waitFor({ state: "hidden", timeout: DEFAULT_TIMEOUT }).catch(() => undefined);
    }
  }

  async cleanupStartedCurtailment(target: CurtailmentCleanupTarget) {
    await this.page.goto("/energy");
    await expect(this.page).toHaveURL(/.*\/energy/);

    if (!(await this.waitForMatchingCleanupCurtailment(target))) {
      return;
    }

    if (await this.stopMatchingActiveCurtailmentIfPresent(target.reason)) {
      await this.waitForCurtailmentToRestore(target);
      return;
    }

    if (await this.stopHistoryCurtailmentIfPresent(target)) {
      await this.waitForCurtailmentToRestore(target);
      return;
    }

    if (await this.hasMatchingActiveCurtailment(target.reason)) {
      await this.waitForCurtailmentToRestore(target);
    }
  }

  async cleanupStartedCurtailmentsByReasonPrefix(prefix: string) {
    await this.page.goto("/energy");
    await expect(this.page).toHaveURL(/.*\/energy/);

    const reasons = new Set<string>();
    const activeReasonPattern = new RegExp(`(${escapeRegex(prefix)}.*?)(?=\\s+\\(Applies to )`);

    const activeCurtailmentSections = this.page
      .getByTestId("active-curtailment-primary-lockup")
      .locator("xpath=ancestor::section[1]");

    for (const activeCurtailmentSection of await activeCurtailmentSections.all()) {
      const text = (await activeCurtailmentSection.textContent()) ?? "";
      const activeReasonMatch = text.match(activeReasonPattern);
      if (activeReasonMatch?.[1]) {
        reasons.add(activeReasonMatch[1].trim());
      }
    }

    const stoppableHistoryRows = this.page
      .locator('[data-testid^="curtailment-history-row-"]')
      .filter({ has: this.page.getByRole("button", { name: /^Stop / }) });

    for (const historyRow of await stoppableHistoryRows.all()) {
      const text = (await historyRow.textContent()) ?? "";
      for (const line of text.split("\n")) {
        const trimmed = line.trim();
        if (trimmed.startsWith(prefix)) {
          reasons.add(trimmed);
        }
      }
    }

    for (const reason of reasons) {
      await this.cleanupStartedCurtailment({ reason });
    }
  }

  private activeCurtailmentSection(reason: string): Locator {
    return this.page
      .getByTestId("active-curtailment-primary-lockup")
      .locator("xpath=ancestor::section[1]")
      .filter({ hasText: reason });
  }

  private activeCurtailmentStopButton(reason: string): Locator {
    return this.activeCurtailmentSection(reason).getByRole("button", { name: "Stop", exact: true });
  }

  private activeCurtailmentRestoreButton(reason: string): Locator {
    return this.activeCurtailmentSection(reason).getByRole("button", { name: "Restore", exact: true });
  }

  private curtailmentHistoryRowByReason(reason: string): Locator {
    return this.page
      .locator('[data-testid^="curtailment-history-row-"]')
      .filter({ has: this.page.getByText(reason, { exact: true }) });
  }

  private curtailmentHistoryRow({ reason, eventUuid }: CurtailmentCleanupTarget): Locator {
    if (eventUuid) {
      return this.page.getByTestId(`curtailment-history-row-${eventUuid}`);
    }

    return this.curtailmentHistoryRowByReason(reason);
  }

  private curtailmentHistoryStopButton(target: CurtailmentCleanupTarget): Locator {
    return this.curtailmentHistoryRow(target).getByRole("button", { name: `Stop ${target.reason}` });
  }

  private async stopHistoryCurtailmentIfPresent(target: CurtailmentCleanupTarget): Promise<boolean> {
    const historyStopButton = this.curtailmentHistoryStopButton(target);
    if ((await historyStopButton.count()) === 0) {
      return false;
    }

    await historyStopButton.first().scrollIntoViewIfNeeded();
    await this.confirmStopAction(historyStopButton.first(), "Confirm stop");
    return true;
  }

  private async hasMatchingStoppableCurtailment(target: CurtailmentCleanupTarget): Promise<boolean> {
    if (
      await this.activeCurtailmentStopButton(target.reason)
        .isVisible()
        .catch(() => false)
    ) {
      return true;
    }

    if (
      await this.activeCurtailmentRestoreButton(target.reason)
        .isVisible()
        .catch(() => false)
    ) {
      return true;
    }

    return (await this.curtailmentHistoryStopButton(target).count()) > 0;
  }

  private async hasMatchingActiveCurtailment(reason: string): Promise<boolean> {
    return this.activeCurtailmentSection(reason)
      .isVisible()
      .catch(() => false);
  }

  private async hasMatchingCleanupCurtailment(target: CurtailmentCleanupTarget): Promise<boolean> {
    return (
      (await this.hasMatchingActiveCurtailment(target.reason)) ||
      (await this.curtailmentHistoryStopButton(target).count()) > 0
    );
  }

  private async waitForMatchingCleanupCurtailment(target: CurtailmentCleanupTarget): Promise<boolean> {
    const deadline = Date.now() + DEFAULT_TIMEOUT;

    do {
      if (await this.hasMatchingCleanupCurtailment(target)) {
        return true;
      }

      await delay(DEFAULT_INTERVAL);
    } while (Date.now() < deadline);

    return this.hasMatchingCleanupCurtailment(target);
  }

  private async confirmStopAction(button: Locator, confirmationButtonName: "Confirm stop" | "Restore power") {
    const stopRequest = this.page.waitForRequest(stopRequestPattern);
    const stopResponse = this.page.waitForResponse(stopRequestPattern);
    const confirmationButton = this.page.getByRole("button", { name: confirmationButtonName });

    await button.click();
    await confirmationButton.click();
    await stopRequest;
    await stopResponse;
    await expect(confirmationButton).toBeHidden();
  }
}

export function getStartCurtailmentRequestBody(request: Request) {
  return request.postDataJSON() as {
    fixedKw?: { targetKw?: number; toleranceKw?: number };
    forceIncludeMaintenance?: boolean;
    includeMaintenance?: boolean;
    mode?: string;
    modeParams?: { fixedKw?: { targetKw?: number; toleranceKw?: number } };
    reason?: string;
    restoreBatchIntervalSec?: number;
    scopes?: Array<{ wholeOrg?: Record<string, never> }>;
  };
}

export async function getStartCurtailmentResponseBody(response: Response): Promise<{
  event?: {
    eventUuid?: string;
    reason?: string;
  };
}> {
  return (await response.json()) as {
    event?: {
      eventUuid?: string;
      reason?: string;
    };
  };
}
