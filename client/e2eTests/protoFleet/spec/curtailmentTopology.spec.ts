import type { Browser, Request, Response, TestInfo } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import {
  type BuildingsScenarioData,
  createBuildingsScenarioData,
  setupRackAssignedToBuilding,
} from "../helpers/buildingsTestSetup";
import { CommonSteps } from "../helpers/commonSteps";
import { getSafeProjectName, getTestRunKey, installAllSitesInitScript } from "../helpers/fleetLocationsSetup";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import {
  type CurtailmentCleanupTarget,
  type CurtailmentScopeJson,
  EnergyPage,
  getStartCurtailmentRequestBody,
  getStartCurtailmentResponseBody,
} from "../pages/energy";
import { FleetLocationsPage } from "../pages/fleetLocations";
import { GroupsPage } from "../pages/groups";
import { MinersPage } from "../pages/miners";
import { RacksPage } from "../pages/racks";
import { SettingsCurtailmentPage } from "../pages/settingsCurtailment";

const PROFILE_PREFIX = "curtailment_topology_profile_e2e";
const GROUP_PREFIX = "curtailment_topology_group_e2e";
const REASON_PREFIX = "curtailment_topology_run_e2e";

type ResponseProfileRequestBody = {
  forceIncludeAllPairedMiners?: boolean;
  forceIncludeMaintenance?: boolean;
  includeMaintenance?: boolean;
  mode?: string;
  profileName?: string;
  scopeSchemaVersion?: number;
  scopes?: CurtailmentScopeJson[];
};

type TopologyTestState = {
  energyReason: string;
  groupName: string;
  profileName: string;
  scenario: BuildingsScenarioData;
};

const topologyTestStates = new Map<string, TopologyTestState>();

function getTopologyTestState(testInfo: TestInfo): TopologyTestState {
  const state = topologyTestStates.get(getTestRunKey(testInfo));
  if (!state) {
    throw new Error("Curtailment topology E2E state was not initialized.");
  }

  return state;
}

function createTopologyTestState(testInfo: TestInfo): TopologyTestState {
  const runSuffix = `${getSafeProjectName(testInfo)}_${testInfo.workerIndex}_${testInfo.retry}`;
  return {
    energyReason: generateRandomText(`${REASON_PREFIX}_${runSuffix}`),
    groupName: generateRandomText(`${GROUP_PREFIX}_${runSuffix}`),
    profileName: generateRandomText(`${PROFILE_PREFIX}_${runSuffix}`),
    scenario: createBuildingsScenarioData(),
  };
}

function getRequestBody(request: Request): Record<string, unknown> {
  try {
    const body = request.postDataJSON();
    return typeof body === "object" && body !== null ? (body as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function isResponseProfileRequest(request: Request, methodName: string, profileName: string): boolean {
  if (request.method() !== "POST" || !request.url().includes(methodName)) {
    return false;
  }

  return getRequestBody(request).profileName === profileName;
}

function expectOnlyTopologyScope(request: Request, expectedScope: CurtailmentScopeJson): ResponseProfileRequestBody {
  const body = getRequestBody(request) as ResponseProfileRequestBody;
  expect(body.scopeSchemaVersion).toBe(1);
  expect(body.scopes).toEqual([expectedScope]);
  return body;
}

function expectProfileReference(body: ReturnType<typeof getStartCurtailmentRequestBody>) {
  expect(body.responseProfileId).toEqual(expect.stringMatching(/\S/));
  expect(body.expectedResponseProfileRevision).toEqual(expect.stringMatching(/\S/));
}

function expectGroupProfileStart(request: Request, groupId: bigint, reason?: string) {
  const body = getStartCurtailmentRequestBody(request);
  expect(body.scopeSchemaVersion).toBe(1);
  expect(body.scopes).toEqual([{ group: { groupId: groupId.toString() } }]);
  expect(body.forceIncludeAllPairedMiners).toBe(true);
  expectProfileReference(body);
  if (reason) {
    expect(body.reason).toBe(reason);
  }
}

async function getStartedCurtailment(response: Response, expectedReason?: string): Promise<CurtailmentCleanupTarget> {
  expect(response.status()).toBe(200);
  const body = await getStartCurtailmentResponseBody(response);
  expect(body.event?.eventUuid).toEqual(expect.stringMatching(/\S/));
  expect(body.event?.reason).toEqual(expect.stringMatching(/\S/));
  if (expectedReason) {
    expect(body.event?.reason).toBe(expectedReason);
  }

  return { reason: body.event!.reason!, eventUuid: body.event!.eventUuid! };
}

async function cleanupTopologyTest(browser: Browser, testInfo: TestInfo, state: TopologyTestState) {
  const context = await browser.newContext({
    baseURL: testConfig.baseUrl,
    viewport: testInfo.project.use?.viewport,
  });

  try {
    const page = await context.newPage();
    await installAllSitesInitScript(page);
    await page.goto("/");

    const isMobile = testInfo.project.use?.isMobile ?? false;
    const authPage = new AuthPage(page, isMobile);
    const minersPage = new MinersPage(page, isMobile);
    const commonSteps = new CommonSteps(authPage, minersPage);
    const energyPage = new EnergyPage(page, isMobile);
    const settingsCurtailmentPage = new SettingsCurtailmentPage(page, isMobile);
    const groupsPage = new GroupsPage(page, isMobile);
    const racksPage = new RacksPage(page, isMobile);
    const fleetLocationsPage = new FleetLocationsPage(page, isMobile);

    await commonSteps.loginAsAdmin();
    await energyPage.cleanupStartedCurtailmentsByReasonPrefix(state.profileName);
    await energyPage.cleanupStartedCurtailmentsByReasonPrefix(state.energyReason);
    await settingsCurtailmentPage.navigateToCurtailmentSettings();
    await settingsCurtailmentPage.deleteResponseProfilesByPrefix(state.profileName);
    await groupsPage.navigateToGroupsPage();
    await groupsPage.waitForSavedGroupsListToLoad();
    await groupsPage.deleteSavedGroupIfVisible(state.groupName);
    await racksPage.deleteRackByLabelIfVisible(state.scenario.rackLabel);
    await fleetLocationsPage.deleteBuildingByNameIfVisible(state.scenario.buildingName);
    await fleetLocationsPage.deleteSiteByNameIfVisible(state.scenario.siteName);
  } finally {
    await context.close();
  }
}

test.describe("Proto Fleet - Curtailment topology targets", () => {
  test.beforeEach(async ({ commonSteps, page }, testInfo) => {
    topologyTestStates.set(getTestRunKey(testInfo), createTopologyTestState(testInfo));
    await installAllSitesInitScript(page);
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test.afterEach("CLEANUP: Restore curtailments and delete topology fixtures", async ({ browser }, testInfo) => {
    const runKey = getTestRunKey(testInfo);
    const state = topologyTestStates.get(runKey);
    if (!state) {
      return;
    }

    try {
      await cleanupTopologyTest(browser, testInfo, state);
    } finally {
      topologyTestStates.delete(runKey);
    }
  });

  if (testConfig.target !== "real") {
    test(
      "Create, edit, test, and run a topology-scoped response profile",
      { tag: "@smoke" },
      async ({ energyPage, fleetLocationsPage, groupsPage, page, racksPage, settingsCurtailmentPage }, testInfo) => {
        test.setTimeout(testConfig.testTimeout * 4);
        const state = getTopologyTestState(testInfo);

        const { buildingId, rackId, selectedMinerIps } = await setupRackAssignedToBuilding(
          page,
          fleetLocationsPage,
          racksPage,
          state.scenario,
        );
        const groupId = await groupsPage.createGroupWithMiners(state.groupName, selectedMinerIps);

        await test.step("Create a building-scoped response profile", async () => {
          await settingsCurtailmentPage.navigateToCurtailmentSettings();
          await settingsCurtailmentPage.openCreateResponseProfile();
          await settingsCurtailmentPage.fillResponseProfile({
            name: state.profileName,
            curtailBatchSize: "2",
            curtailBatchIntervalSec: "1",
            restoreBatchSize: "2",
            restoreBatchIntervalSec: "1",
          });
          await settingsCurtailmentPage.selectSiteTarget(state.scenario.siteName);
          await settingsCurtailmentPage.selectBuildingTarget(state.scenario.buildingName);
          await settingsCurtailmentPage.enableAllPairedTargeting();

          const createRequestPromise = page.waitForRequest((request) =>
            isResponseProfileRequest(request, "CreateCurtailmentResponseProfile", state.profileName),
          );
          await settingsCurtailmentPage.saveResponseProfile();
          const createRequest = await createRequestPromise;
          const createBody = expectOnlyTopologyScope(createRequest, {
            building: { buildingId: buildingId.toString() },
          });

          expect(createBody.mode).toBe("CURTAILMENT_MODE_FULL_FLEET");
          expect(createBody.forceIncludeAllPairedMiners).toBe(true);
          expect(createBody.includeMaintenance).toBe(true);
          expect(createBody.forceIncludeMaintenance).toBe(true);
          await settingsCurtailmentPage.validateResponseProfileVisible(state.profileName, "1 building");
        });

        await test.step("Reload the profile and replace its building scope with the selected rack", async () => {
          await settingsCurtailmentPage.reloadResponseProfiles();
          await settingsCurtailmentPage.openEditResponseProfile(state.profileName);
          await settingsCurtailmentPage.validateBuildingTargetSelected();
          await settingsCurtailmentPage.selectSiteTarget(state.scenario.siteName);
          await settingsCurtailmentPage.selectBuildingTarget(state.scenario.buildingName);
          await settingsCurtailmentPage.selectRackTarget(state.scenario.rackLabel);

          const updateRequestPromise = page.waitForRequest((request) =>
            isResponseProfileRequest(request, "UpdateCurtailmentResponseProfile", state.profileName),
          );
          await settingsCurtailmentPage.saveResponseProfile();
          const updateRequest = await updateRequestPromise;
          const updateBody = expectOnlyTopologyScope(updateRequest, { rack: { rackId: rackId.toString() } });
          expect(updateBody.forceIncludeAllPairedMiners).toBe(true);
          await settingsCurtailmentPage.validateResponseProfileVisible(state.profileName, "1 rack");
        });

        await test.step("Reload the profile, switch it to the selected group, and test it", async () => {
          await settingsCurtailmentPage.reloadResponseProfiles();
          await settingsCurtailmentPage.openEditResponseProfile(state.profileName);
          await settingsCurtailmentPage.validateRackTargetSelected();
          await settingsCurtailmentPage.selectSiteTarget(state.scenario.siteName);
          await settingsCurtailmentPage.selectGroupTarget(state.groupName);

          const updateRequestPromise = page.waitForRequest((request) =>
            isResponseProfileRequest(request, "UpdateCurtailmentResponseProfile", state.profileName),
          );
          const startRequestPromise = page.waitForRequest(/StartCurtailment/);
          const startResponsePromise = page.waitForResponse(/StartCurtailment/);
          await settingsCurtailmentPage.runResponseProfileCurtailment();

          const updateRequest = await updateRequestPromise;
          const updateBody = expectOnlyTopologyScope(updateRequest, { group: { groupId: groupId.toString() } });
          expect(updateBody.forceIncludeAllPairedMiners).toBe(true);

          const startRequest = await startRequestPromise;
          expectGroupProfileStart(startRequest, groupId);
          const testedCurtailment = await getStartedCurtailment(await startResponsePromise);
          await energyPage.validateActiveCurtailment(testedCurtailment.reason);
          await energyPage.stopCurtailment(testedCurtailment);
          await energyPage.waitForCurtailmentToRestore(testedCurtailment);
        });

        await test.step("Select the saved group profile from New curtailment and run it", async () => {
          await energyPage.openCurtailmentPlanner();
          await energyPage.selectResponseProfile(state.profileName);
          await energyPage.validateGroupTargetSelected();
          await energyPage.fillCurtailmentReason(state.energyReason);
          await energyPage.waitForCurtailmentReady();

          const startRequestPromise = page.waitForRequest(/StartCurtailment/);
          const startResponsePromise = page.waitForResponse(/StartCurtailment/);
          await energyPage.startCurtailment();

          const startRequest = await startRequestPromise;
          expectGroupProfileStart(startRequest, groupId, state.energyReason);
          const startedCurtailment = await getStartedCurtailment(await startResponsePromise, state.energyReason);
          await energyPage.validateActiveCurtailment(startedCurtailment.reason);
          await energyPage.validateCurtailmentHistoryRow(startedCurtailment.reason);
          await energyPage.stopCurtailment(startedCurtailment);
          await energyPage.waitForCurtailmentToRestore(startedCurtailment);
        });
      },
    );
  }
});
