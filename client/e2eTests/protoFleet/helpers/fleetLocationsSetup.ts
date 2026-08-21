import { type Page, type TestInfo } from "@playwright/test";

const ACTIVE_SITE_STORAGE_KEY = "proto-fleet-multi-site";

export function getTestRunKey(testInfo: TestInfo): string {
  return [testInfo.project.name, testInfo.testId, testInfo.workerIndex, testInfo.retry, testInfo.repeatEachIndex].join(
    ":",
  );
}

export function getSafeProjectName(testInfo: TestInfo): string {
  return (
    testInfo.project.name
      .replace(/[^a-zA-Z0-9]+/g, "_")
      .replace(/^_+|_+$/g, "")
      .toLowerCase() || "project"
  );
}

export async function installAllSitesInitScript(page: Page) {
  await page.addInitScript(
    ({ storageKey }) => {
      localStorage.setItem(
        storageKey,
        JSON.stringify({
          state: {
            ui: {
              activeSite: { kind: "all" },
            },
          },
          version: 0,
        }),
      );
    },
    { storageKey: ACTIVE_SITE_STORAGE_KEY },
  );
}
