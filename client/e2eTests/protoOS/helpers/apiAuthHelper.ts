import { APIRequestContext, expect, Page } from "@playwright/test";

type WaitForAuthenticatedApiRecoveryParams = {
  accessToken: string;
  path: string;
  request: APIRequestContext;
  timeoutMs: number;
};

export async function getAuthAccessToken(page: Page) {
  return page.evaluate(() => {
    const authData = window.localStorage.getItem("proto-os-auth");
    if (!authData) {
      throw new Error("Missing proto-os-auth in localStorage");
    }

    const parsed = JSON.parse(authData) as {
      state?: {
        auth?: {
          authTokens?: {
            accessToken?: { value?: string };
          };
        };
      };
    };

    const accessToken = parsed.state?.auth?.authTokens?.accessToken?.value;
    if (!accessToken) {
      throw new Error("Missing access token in proto-os-auth");
    }

    return accessToken;
  });
}

export async function waitForAuthenticatedApiRecovery({
  accessToken,
  path,
  request,
  timeoutMs,
}: WaitForAuthenticatedApiRecoveryParams) {
  await expect
    .poll(
      async () => {
        try {
          const response = await request.get(path, {
            headers: {
              Authorization: `Bearer ${accessToken}`,
            },
          });

          return response.status();
        } catch {
          return 0;
        }
      },
      { timeout: timeoutMs },
    )
    .toBe(200);
}
