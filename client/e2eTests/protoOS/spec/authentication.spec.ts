import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { generateStrongPassword, loginViaApi, restoreAdminPassword } from "../helpers/authenticationHelper";

test.describe("Authentication settings", () => {
  let updatedPassword: string | null;

  test.beforeEach(async ({ page, commonSteps }) => {
    updatedPassword = null;
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
  });

  test.afterEach(async ({ page, request }) => {
    if (!updatedPassword) {
      return;
    }

    await restoreAdminPassword(request, updatedPassword);
    updatedPassword = null;
    await page.evaluate(() => {
      window.localStorage.removeItem("proto-os-auth");
    });
    await page.goto("/");
  });

  test("Admin password can be updated from settings and used on the next login", async ({
    authenticationPage,
    commonSteps,
    page,
    request,
  }) => {
    const newPassword = generateStrongPassword();
    const requestPromise = page.waitForRequest(
      (request) => request.method() === "PUT" && request.url().includes("/api/v1/auth/change-password"),
    );
    const responsePromise = page.waitForResponse(
      (response) => response.request().method() === "PUT" && response.url().includes("/api/v1/auth/change-password"),
    );

    await commonSteps.navigateToAuthenticationSettings();

    await test.step("Update the admin password from Authentication settings", async () => {
      await authenticationPage.validateUsernameFieldDisabledWithValue(testConfig.admin.username);
      await authenticationPage.updateAdminPassword(testConfig.admin.password, newPassword);
    });

    await test.step("Validate the password change request and success state", async () => {
      const changePasswordRequest = await requestPromise;
      const changePasswordResponse = await responsePromise;

      expect(changePasswordResponse.status()).toBe(200);
      updatedPassword = newPassword;
      expect(changePasswordRequest.postDataJSON()).toEqual({
        current_password: testConfig.admin.password,
        new_password: newPassword,
      });

      await authenticationPage.validateToastMessage("Password updated");
      await authenticationPage.validateLoggedIn();
    });

    await test.step("Validate the new password can authenticate again", async () => {
      const accessToken = await loginViaApi(request, newPassword);
      expect(accessToken).toBeTruthy();
    });
  });

  test("Authentication settings requires the current password before updating", async ({
    authenticationPage,
    commonSteps,
  }) => {
    const newPassword = generateStrongPassword();

    await commonSteps.navigateToAuthenticationSettings();

    await test.step("Open Authentication settings and review the admin login form", async () => {
      await authenticationPage.validateTitle("Update your admin login");
      await authenticationPage.validateUsernameFieldDisabledWithValue(testConfig.admin.username);
    });

    await test.step("Try to submit a new password without entering the current password", async () => {
      await authenticationPage.inputNewPassword(newPassword);
      await authenticationPage.inputConfirmPassword(newPassword);
      await authenticationPage.clickContinue();
    });

    await test.step("Validate the current password error is shown", async () => {
      await authenticationPage.validateTextIsVisible("Current password is required");
    });
  });
});
