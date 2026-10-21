import { create, toJsonString } from "@bufbuild/protobuf";
import { expect, test } from "../fixtures/pageFixtures";
import {
  ConfirmFleetNodeResponseSchema,
  CreateEnrollmentCodeResponseSchema,
  FleetNodeEnrollmentStatus,
  FleetNodeSummarySchema,
  ListFleetNodesResponseSchema,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";

test.describe("Node enrollment intent", () => {
  test.use({ storageState: { cookies: [], origins: [] } });
  for (const standby of [false, true]) {
    test(`enrolls a ${standby ? "standby for the selected active node" : "standalone node"} through confirmation`, async ({
      page,
      commonSteps,
      settingsNodesPage,
    }) => {
      const codeRequests: unknown[] = [];
      const confirmRequests: unknown[] = [];
      let registered = false;
      let confirmed = false;
      const active = create(FleetNodeSummarySchema, {
        fleetNodeId: 7n,
        name: "active-node",
        enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
        standbyEnrollmentEligible: true,
        controlStreamConnected: true,
        pairedDeviceCount: 25n,
      });
      const unavailable = create(FleetNodeSummarySchema, {
        fleetNodeId: 8n,
        name: "unavailable-node",
        enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
        standbyEnrollmentEligible: false,
      });
      await page.route(/FleetNodeAdminService\//, async (route) => {
        const method = route.request().url().split("/").pop();
        if (method === "ListFleetNodes") {
          const pending = create(FleetNodeSummarySchema, {
            fleetNodeId: 9n,
            pendingEnrollmentId: 11n,
            name: "new-node",
            identityFingerprint: "SHA256:enrollment-test-host",
            enrollmentStatus: confirmed
              ? FleetNodeEnrollmentStatus.CONFIRMED
              : FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION,
            standbyForNodeId: standby ? 7n : undefined,
            standbyForNodeName: standby ? "active-node" : "",
            haRole: standby && confirmed ? "standby" : "",
            haOwnerNodeId: standby && confirmed ? 7n : undefined,
          });
          await route.fulfill({
            contentType: "application/json",
            body: toJsonString(
              ListFleetNodesResponseSchema,
              create(ListFleetNodesResponseSchema, {
                fleetNodes: [active, unavailable, ...(registered ? [pending] : [])],
              }),
            ),
          });
        } else if (method === "CreateEnrollmentCode") {
          codeRequests.push(route.request().postDataJSON());
          await route.fulfill({
            contentType: "application/json",
            body: toJsonString(
              CreateEnrollmentCodeResponseSchema,
              create(CreateEnrollmentCodeResponseSchema, { code: "test-enrollment-code", pendingEnrollmentId: 11n }),
            ),
          });
        } else if (method === "ConfirmFleetNode") {
          confirmRequests.push(route.request().postDataJSON());
          confirmed = true;
          await route.fulfill({
            contentType: "application/json",
            body: toJsonString(
              ConfirmFleetNodeResponseSchema,
              create(ConfirmFleetNodeResponseSchema, { apiKey: "one-time-node-key" }),
            ),
          });
        } else {
          await route.fulfill({
            status: 500,
            contentType: "application/json",
            body: JSON.stringify({ code: "unimplemented", message: `Unexpected node RPC: ${method}` }),
          });
        }
      });

      await test.step("Choose enrollment intent before creating a code", async () => {
        await page.goto("/");
        await commonSteps.loginAsAdmin();
        await settingsNodesPage.navigateToNodesSettings();
        await settingsNodesPage.waitForNodesListToLoad();
        await settingsNodesPage.clickEnrollNode();
        await settingsNodesPage.validateEnrollNodeModalOpened();
        expect(codeRequests).toEqual([]);
        // eslint-disable-next-line playwright/no-conditional-in-test -- the two deterministic cases share the confirmation flow.
        if (standby) await settingsNodesPage.chooseStandbyFor("active-node", "unavailable-node");
        await settingsNodesPage.continueEnrollment();
        await settingsNodesPage.validateEnrollmentCode("test-enrollment-code");
        expect(codeRequests).toEqual([standby ? { standbyForNodeId: "7" } : {}]);
      });

      await test.step("Confirm the registered host and show the one-time API key", async () => {
        registered = true;
        await settingsNodesPage.validateConfirmNodeModalOpened("new-node");
        // eslint-disable-next-line playwright/no-conditional-in-test -- only the standby case has an enrollment relationship.
        if (standby) await settingsNodesPage.validateStandbyRelationship("active-node");
        await settingsNodesPage.confirmNode();
        await settingsNodesPage.finishEnrollment("one-time-node-key");
        expect(confirmRequests).toEqual([{ fleetNodeId: "9", pendingEnrollmentId: "11" }]);
        await settingsNodesPage.validateNodeVisible("new-node");
      });
    });
  }
});
