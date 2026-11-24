import { expect, test } from "../fixtures/pageFixtures";

test.use({ storageState: { cookies: [], origins: [] } });

test("Denied role save stays visible when the permission list is scrolled down", async ({
  page,
  commonSteps,
  settingsPage,
  settingsTeamPage,
  isMobile,
}) => {
  await page.setViewportSize({ width: isMobile ? 393 : 1000, height: 633 });
  await page.goto("/");
  await commonSteps.loginAsAdmin();

  // Use the real catalog and editor, intercepting only role data and the save
  // denial. This scenario never changes roles on the backing server.
  const catalogResponse = await page.request.post("/api-proxy/authz.v1.AuthzService/ListPermissions", {
    data: {},
    headers: { "Connect-Protocol-Version": "1" },
  });
  expect(catalogResponse.ok()).toBeTruthy();
  const { permissions } = (await catalogResponse.json()) as { permissions: { key: string }[] };
  const permissionKeys = permissions.map(({ key }) => key).filter((key) => key !== "account:manage");
  await page.route(/AuthzService\/ListRoles$/, (route) =>
    route.fulfill({ json: { roles: [{ roleId: "save-feedback-role", name: "Save feedback role", permissionKeys }] } }),
  );
  await page.route(/AuthzService\/UpdateCustomRole$/, (route) =>
    route.fulfill({
      status: 403,
      json: {
        code: "permission_denied",
        message: "cannot grant account:manage: caller does not hold this permission at org scope",
      },
    }),
  );

  await settingsPage.navigateToTeamSettings();
  await settingsTeamPage.validateTeamSettingsPageOpened();
  await settingsTeamPage.openRolesTab();
  await settingsTeamPage.clickRoleActionsMenu("Save feedback role");
  await settingsTeamPage.clickEditRoleAction();
  await settingsTeamPage.selectAccountManagementAtBottom();

  const saveRequest = page.waitForRequest(/AuthzService\/UpdateCustomRole$/);
  await settingsTeamPage.clickSaveRoleChanges();
  expect((await saveRequest).postDataJSON()).toMatchObject({
    roleId: "save-feedback-role",
    permissionKeys: expect.arrayContaining(["account:manage"]),
  });
  await settingsTeamPage.validateRoleSaveDenied();
});
