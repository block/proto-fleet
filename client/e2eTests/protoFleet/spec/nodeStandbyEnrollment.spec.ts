import { create, toJsonString } from "@bufbuild/protobuf";
import { expect, test } from "../fixtures/pageFixtures";
import { createNodeSummary, fulfillFleetNodes } from "../helpers/rbacAdminTestSetup";
import {
  ConfirmFleetNodeResponseSchema,
  CreateEnrollmentCodeResponseSchema,
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
  FleetNodeSummarySchema,
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
        controlStreamConnected: true,
        pairedDeviceCount: 25n,
      });
      const alreadyPaired = create(FleetNodeSummarySchema, {
        fleetNodeId: 8n,
        name: "paired-node",
        enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
        haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE,
        haPeerNodeId: 12n,
        haPeerNodeName: "paired-standby",
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
            haRole: standby
              ? FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY
              : FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
            haPeerNodeId: standby ? 7n : undefined,
            haPeerNodeName: standby ? "active-node" : "",
          });
          await fulfillFleetNodes(route, [active, alreadyPaired, ...(registered ? [pending] : [])]);
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
        if (standby) await settingsNodesPage.chooseStandbyFor("active-node", "paired-node");
        await settingsNodesPage.generateEnrollmentCode();
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

test.describe("Node HA pair display", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("shows roles, a preparing standby, and role-specific revoke copy", async ({
    page,
    commonSteps,
    settingsNodesPage,
  }) => {
    const lastSeenAt = new Date();
    await page.route(/FleetNodeAdminService\/ListFleetNodes/, async (route) =>
      fulfillFleetNodes(route, [
        createNodeSummary({
          fleetNodeId: 7n,
          name: "active-node",
          enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
          identityFingerprint: "SHA256:ha-active",
          lastSeenAt,
          extra: {
            controlStreamConnected: true,
            haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE,
            haPeerNodeId: 8n,
            haPeerNodeName: "standby-node",
            haStandbyReady: false,
            haStatusReason: "Waiting for both nodes to connect",
          },
        }),
        createNodeSummary({
          fleetNodeId: 8n,
          name: "standby-node",
          enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
          identityFingerprint: "SHA256:ha-standby",
          lastSeenAt,
          extra: {
            controlStreamConnected: true,
            haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
            haPeerNodeId: 7n,
            haPeerNodeName: "active-node",
            haStandbyReady: false,
            haStatusReason: "Waiting for both nodes to connect",
          },
        }),
      ]),
    );
    await page.route(/FleetNodeAdminService\/ListFleetNodeDevices/, async (route) =>
      route.fulfill({ contentType: "application/json", body: "{}" }),
    );

    await test.step("Show each member's role in the Nodes list", async () => {
      await page.goto("/");
      await commonSteps.loginAsAdmin();
      await settingsNodesPage.navigateToNodesSettings();
      await settingsNodesPage.waitForNodesListToLoad();
      await settingsNodesPage.validateNodeRowText("active-node", "Active");
      await settingsNodesPage.validateNodeRowText("standby-node", "Preparing");
    });

    await test.step("Show HA details and block discovery on the standby", async () => {
      await settingsNodesPage.openNodeDetails("standby-node");
      await settingsNodesPage.validateNodeDetailsHa("Preparing", "Standby for", "active-node");
      await settingsNodesPage.validateNodeDetailsHaStatus("Waiting for both nodes to connect");
      await settingsNodesPage.validateFindMinersDisabledForStandby();
      await page.keyboard.press("Escape");
    });

    await test.step("Warn that an unready standby cannot take over when revoking the active node", async () => {
      await settingsNodesPage.openNodeDetails("active-node");
      await settingsNodesPage.clickNodeAction("Revoke");
      await settingsNodesPage.validateRevokeDialogText("active-node", "since standby-node isn't ready to take over");
      await page.keyboard.press("Escape");
    });

    await test.step("Say the peer keeps its devices when revoking the standby", async () => {
      await settingsNodesPage.openNodeDetails("standby-node");
      await settingsNodesPage.clickNodeAction("Revoke");
      await settingsNodesPage.validateRevokeDialogText("standby-node", "active-node keeps its devices");
    });
  });
});
