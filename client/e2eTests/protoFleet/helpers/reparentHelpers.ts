import { expect, type Page, type Response as PlaywrightResponse } from "@playwright/test";
import { type MinersPage } from "../pages/miners";
import { type RacksPage } from "../pages/racks";
import { type CommonSteps } from "./commonSteps";

const LIST_MINERS_RESPONSE = "ListMinerStateSnapshots";
const ACTIVE_SITE_STORAGE_KEY = "proto-fleet-multi-site";

type PlacementRefSnapshot = {
  label?: string;
};

type VisibleMinerSnapshot = {
  deviceIdentifier?: string;
  ipAddress: string;
  rackPosition?: string;
  placement?: {
    site?: PlacementRefSnapshot;
    rack?: PlacementRefSnapshot;
  };
};

export type ReparentMiner = {
  deviceIdentifier?: string;
  ipAddress: string;
  originalRackLabel?: string;
  originalRackPosition?: string;
  originalSiteLabel?: string;
};

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

export async function captureMovableMiner({
  page,
  commonSteps,
  minersPage,
}: {
  page: Page;
  commonSteps: CommonSteps;
  minersPage: MinersPage;
}): Promise<ReparentMiner> {
  const snapshots = await loadVisibleMiners({ page, commonSteps });
  const ipAddresses = await minersPage.getVisibleMinerIpAddresses();

  for (const ipAddress of ipAddresses) {
    if (!(await minersPage.hasBlankBuildingAndRack(ipAddress))) {
      continue;
    }

    if (await minersPage.hasAuthenticationRequiredIssue(ipAddress)) {
      continue;
    }

    const snapshot = snapshots.find((candidate) => candidate.ipAddress === ipAddress);
    return {
      deviceIdentifier: snapshot?.deviceIdentifier,
      ipAddress,
      originalSiteLabel: normalizePlacementLabel(await minersPage.getMinerColumnText(ipAddress, "site")),
      originalRackLabel: snapshot?.placement?.rack?.label,
      originalRackPosition: snapshot?.rackPosition,
    };
  }

  throw new Error("Expected at least one visible miner without building or rack placement for reparent coverage");
}

export function expectSingleDeviceIdentifier(actual: string[] | undefined, expected?: string) {
  if (expected) {
    expect(actual).toEqual([expected]);
    return;
  }

  expect(actual).toHaveLength(1);
}

export async function restoreMinerPlacementIfNeeded({
  page,
  minersPage,
  racksPage,
  miner,
}: {
  page: Page;
  minersPage: MinersPage;
  racksPage: RacksPage;
  miner: ReparentMiner | undefined;
}) {
  if (!miner) {
    return;
  }

  if (!miner.originalRackLabel) {
    await restoreMinerSiteIfNeeded({ page, minersPage, miner });
    return;
  }

  await restoreMinerSiteIfNeeded({ page, minersPage, miner });

  if (miner.originalRackPosition) {
    await restoreMinerRackSlotIfNeeded({ page, racksPage, miner });
    return;
  }

  await restoreMinerRackIfNeeded({ page, minersPage, miner });
}

async function loadVisibleMiners({
  page,
  commonSteps,
}: {
  page: Page;
  commonSteps: CommonSteps;
}): Promise<VisibleMinerSnapshot[]> {
  const responsePromise = new Promise<VisibleMinerSnapshot[]>((resolve, reject) => {
    const timeoutId = setTimeout(() => {
      page.off("response", handleResponse);
      resolve([]);
    }, 10000);

    const handleResponse = async (response: PlaywrightResponse) => {
      if (response.request().method() !== "POST" || !response.url().includes(LIST_MINERS_RESPONSE)) {
        return;
      }

      try {
        const body = (await response.json()) as {
          miners?: VisibleMinerSnapshot[];
          snapshots?: VisibleMinerSnapshot[];
        };
        const snapshots = body.snapshots ?? body.miners ?? [];
        if (snapshots.length === 0) {
          return;
        }

        clearTimeout(timeoutId);
        page.off("response", handleResponse);
        resolve(snapshots);
      } catch (error) {
        clearTimeout(timeoutId);
        page.off("response", handleResponse);
        reject(error);
      }
    };

    page.on("response", handleResponse);
  });

  await commonSteps.goToMinersPage();
  return await responsePromise;
}

function normalizePlacementLabel(value: string | undefined): string | undefined {
  const normalized = value?.trim();
  if (!normalized || normalized === "—") {
    return undefined;
  }

  return normalized;
}

async function restoreMinerSiteIfNeeded({
  page,
  minersPage,
  miner,
}: {
  page: Page;
  minersPage: MinersPage;
  miner: ReparentMiner;
}) {
  if (!miner.originalSiteLabel) {
    return;
  }

  await page.goto("/fleet/miners");
  await minersPage.waitForMinersListToLoad();
  await minersPage.clickMinerCheckbox(miner.ipAddress);
  await minersPage.validateActionBarMinerCount(1);
  await minersPage.assignSelectedMinersToSite(miner.originalSiteLabel);
}

async function restoreMinerRackSlotIfNeeded({
  page,
  racksPage,
  miner,
}: {
  page: Page;
  racksPage: RacksPage;
  miner: ReparentMiner;
}) {
  if (!miner.originalRackLabel || !miner.originalRackPosition) {
    return;
  }

  const slotNumber = Number(miner.originalRackPosition);
  if (Number.isNaN(slotNumber)) {
    throw new Error(`Could not parse original rack position "${miner.originalRackPosition}"`);
  }

  await page.goto("/fleet/racks");
  await racksPage.clickViewList();
  await racksPage.waitForRackListToLoad({ allowEmpty: false });
  await racksPage.openRackFromList(miner.originalRackLabel);
  await racksPage.clickRackOverviewEmptySlot(slotNumber);
  await racksPage.assignSearchMinerByIpAddress(miner.ipAddress);
  await racksPage.validateRackOverviewAssignedSlots([slotNumber]);
}

async function restoreMinerRackIfNeeded({
  page,
  minersPage,
  miner,
}: {
  page: Page;
  minersPage: MinersPage;
  miner: ReparentMiner;
}) {
  if (!miner.originalRackLabel) {
    return;
  }

  await page.goto("/fleet/miners");
  await minersPage.waitForMinersListToLoad();
  await minersPage.clickMinerCheckbox(miner.ipAddress);
  await minersPage.validateActionBarMinerCount(1);
  await minersPage.assignSelectedMinersToRack(miner.originalRackLabel);
}
