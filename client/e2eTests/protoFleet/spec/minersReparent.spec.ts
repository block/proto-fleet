import { test } from "../fixtures/pageFixtures";
import {
  captureMovableMiner,
  expectSingleDeviceIdentifier,
  installAllSitesInitScript,
  type ReparentMiner,
  restoreMinerPlacementIfNeeded,
} from "../helpers/reparentHelpers";
import { generateRandomText } from "../helpers/testDataHelper";

const ASSIGN_DEVICES_TO_SITE = "AssignDevicesToSite";
const ASSIGN_DEVICES_TO_RACK = "AssignDevicesToRack";
const ASSIGN_RACKS_TO_BUILDING = "AssignRacksToBuilding";
const ASSIGN_RACKS_TO_SITE = "AssignRacksToSite";
const TEMP_ZONE = "ReparentAutomationZone";
const RACK_COLUMNS = 2;
const RACK_ROWS = 2;

async function deleteRackIfCreated({
  created,
  rackLabel,
  racksPage,
}: {
  created: boolean;
  rackLabel: string;
  racksPage: { deleteRackByLabelIfVisible(label: string): Promise<void> };
}) {
  if (!created) {
    return;
  }

  await racksPage.deleteRackByLabelIfVisible(rackLabel);
}

async function deleteBuildingIfCreated({
  buildingName,
  created,
  fleetLocationsPage,
}: {
  buildingName: string;
  created: boolean;
  fleetLocationsPage: { deleteBuilding(name: string): Promise<void> };
}) {
  if (!created) {
    return;
  }

  await fleetLocationsPage.deleteBuilding(buildingName);
}

async function deleteSiteIfCreated({
  created,
  fleetLocationsPage,
  siteName,
}: {
  created: boolean;
  fleetLocationsPage: { deleteSite(name: string): Promise<void> };
  siteName: string;
}) {
  if (!created) {
    return;
  }

  await fleetLocationsPage.deleteSite(siteName);
}

test.describe("Miners reparent", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await installAllSitesInitScript(page);
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test("Move a rack to a building from the Racks tab", async ({ page, fleetLocationsPage, racksPage }) => {
    const siteName = generateRandomText("reparent_site");
    const buildingName = generateRandomText("reparent_building");
    const rackLabel = generateRandomText("reparent_rack");

    let createdSite = false;
    let createdBuilding = false;
    let createdRack = false;

    try {
      await fleetLocationsPage.createSite(siteName);
      createdSite = true;
      const buildingId = await fleetLocationsPage.createBuilding(siteName, buildingName);
      createdBuilding = true;
      const rackId = await racksPage.createRack({
        columns: RACK_COLUMNS,
        label: rackLabel,
        rows: RACK_ROWS,
        zone: TEMP_ZONE,
      });
      createdRack = true;

      const requestPromise = page.waitForRequest(new RegExp(ASSIGN_RACKS_TO_BUILDING));
      const responsePromise = page.waitForResponse(new RegExp(ASSIGN_RACKS_TO_BUILDING));

      await test.step("Move the rack into the target building", async () => {
        await racksPage.assignRackToBuildingFromList(rackLabel, buildingName);
      });

      await test.step("Validate the rack move request and resulting row placement", async () => {
        const request = await requestPromise;
        const response = await responsePromise;
        const body = request.postDataJSON() as {
          targetBuildingId?: string;
          racks: Array<{ rackId?: string }>;
        };

        test.expect(String(body.targetBuildingId)).toBe(buildingId.toString());
        test.expect(body.racks).toHaveLength(1);
        test.expect(String(body.racks[0]?.rackId)).toBe(rackId.toString());
        test.expect(response.status()).toBe(200);

        await racksPage.navigateToRacksPage();
        await racksPage.clickViewList();
        await racksPage.waitForRackListToLoad({ allowEmpty: false });
        await racksPage.validateRackPlacementRow(rackLabel, siteName, buildingName);
      });
    } finally {
      await deleteRackIfCreated({ created: createdRack, rackLabel, racksPage });
      await deleteBuildingIfCreated({ buildingName, created: createdBuilding, fleetLocationsPage });
      await deleteSiteIfCreated({ created: createdSite, fleetLocationsPage, siteName });
    }
  });

  test("Assign a single miner to a site from the Miners tab", async ({
    page,
    commonSteps,
    fleetLocationsPage,
    minersPage,
    racksPage,
  }) => {
    const targetSiteName = generateRandomText("miner_site");

    let createdSite = false;
    let miner: ReparentMiner | undefined;

    try {
      miner = await captureMovableMiner({ page, commonSteps, minersPage });
      const targetSiteId = await fleetLocationsPage.createSite(targetSiteName);
      createdSite = true;

      const requestPromise = page.waitForRequest(new RegExp(ASSIGN_DEVICES_TO_SITE));
      const responsePromise = page.waitForResponse(new RegExp(ASSIGN_DEVICES_TO_SITE));

      await page.goto("/fleet/miners");
      await minersPage.waitForMinersListToLoad();

      await test.step("Move one miner into the target site", async () => {
        await minersPage.clickMinerCheckbox(miner.ipAddress);
        await minersPage.validateActionBarMinerCount(1);
        await minersPage.assignSelectedMinersToSite(targetSiteName);
      });

      await test.step("Validate the request and the target site miner count", async () => {
        const request = await requestPromise;
        const response = await responsePromise;
        const body = request.postDataJSON() as {
          targetSiteId?: string;
          deviceIdentifiers?: string[];
        };

        test.expect(String(body.targetSiteId)).toBe(targetSiteId.toString());
        expectSingleDeviceIdentifier(body.deviceIdentifiers, miner.deviceIdentifier);
        test.expect(response.status()).toBe(200);

        await fleetLocationsPage.validateSiteMinerCount(targetSiteName, 1);
      });
    } finally {
      await restoreMinerPlacementIfNeeded({ page, minersPage, racksPage, miner });
      await deleteSiteIfCreated({ created: createdSite, fleetLocationsPage, siteName: targetSiteName });
    }
  });

  test("Assign a single miner to a rack from the Miners tab", async ({
    page,
    commonSteps,
    fleetLocationsPage,
    minersPage,
    racksPage,
  }) => {
    const targetSiteName = generateRandomText("rack_site");
    const rackLabel = generateRandomText("miner_rack");

    let createdSite = false;
    let createdRack = false;
    let miner: ReparentMiner | undefined;

    try {
      miner = await captureMovableMiner({ page, commonSteps, minersPage });
      const targetSiteId = await fleetLocationsPage.createSite(targetSiteName);
      createdSite = true;
      const rackId = await racksPage.createRack({
        columns: RACK_COLUMNS,
        label: rackLabel,
        rows: RACK_ROWS,
        zone: TEMP_ZONE,
      });
      createdRack = true;

      const rackSiteRequestPromise = page.waitForRequest(new RegExp(ASSIGN_RACKS_TO_SITE));
      const rackSiteResponsePromise = page.waitForResponse(new RegExp(ASSIGN_RACKS_TO_SITE));

      await test.step("Move the rack into the target site", async () => {
        await racksPage.assignRackToSiteFromList(rackLabel, targetSiteName);
      });

      await test.step("Validate the rack-to-site request", async () => {
        const request = await rackSiteRequestPromise;
        const response = await rackSiteResponsePromise;
        const body = request.postDataJSON() as {
          rackIds?: string[];
          targetSiteId?: string;
        };

        test.expect(body.rackIds).toEqual([rackId.toString()]);
        test.expect(String(body.targetSiteId)).toBe(targetSiteId.toString());
        test.expect(response.status()).toBe(200);
      });

      const requestPromise = page.waitForRequest(new RegExp(ASSIGN_DEVICES_TO_RACK));
      const responsePromise = page.waitForResponse(new RegExp(ASSIGN_DEVICES_TO_RACK));

      await page.goto("/fleet/miners");
      await minersPage.waitForMinersListToLoad();

      await test.step("Move one miner into the target rack", async () => {
        await minersPage.clickMinerCheckbox(miner.ipAddress);
        await minersPage.validateActionBarMinerCount(1);
        await minersPage.assignSelectedMinersToRack(rackLabel);
      });

      await test.step("Validate the request and target rack miner count", async () => {
        const request = await requestPromise;
        const response = await responsePromise;
        const body = request.postDataJSON() as {
          deviceSelector?: {
            deviceList?: {
              deviceIdentifiers?: string[];
            };
          };
          targetRackId?: string;
        };

        test.expect(String(body.targetRackId)).toBe(rackId.toString());
        expectSingleDeviceIdentifier(body.deviceSelector?.deviceList?.deviceIdentifiers, miner.deviceIdentifier);
        test.expect(response.status()).toBe(200);

        await racksPage.navigateToRacksPage();
        await racksPage.clickViewList();
        await racksPage.waitForRackListToLoad({ allowEmpty: false });
        await racksPage.validateRackMembershipRow(rackLabel, targetSiteName, 1);
      });
    } finally {
      await restoreMinerPlacementIfNeeded({ page, minersPage, racksPage, miner });
      await deleteRackIfCreated({ created: createdRack, rackLabel, racksPage });
      await deleteSiteIfCreated({ created: createdSite, fleetLocationsPage, siteName: targetSiteName });
    }
  });
});
