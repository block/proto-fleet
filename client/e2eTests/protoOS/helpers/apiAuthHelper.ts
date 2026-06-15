import { APIRequestContext, expect, Page } from "@playwright/test";

const FAKE_PROTO_RIG_SERIAL_PREFIX = "PROTO-SIM-";

type WaitForAuthenticatedApiRecoveryParams = {
  accessToken: string;
  path: string;
  request: APIRequestContext;
  timeoutMs: number;
};

type SafeSimulatorTargetParams = {
  actionDescription: string;
  request: APIRequestContext;
};

async function getAuthenticatedApiStatus({
  accessToken,
  path,
  request,
}: Omit<WaitForAuthenticatedApiRecoveryParams, "timeoutMs">) {
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
}

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

export async function assertSafeSimulatorTarget({ actionDescription, request }: SafeSimulatorTargetParams) {
  const response = await request.get("/api/v1/system");
  expect(response.ok()).toBeTruthy();

  const data = (await response.json()) as {
    "system-info": {
      cb_sn?: string;
      product_name?: string;
      manufacturer?: string;
      model?: string;
    };
  };

  const systemInfo = data["system-info"];
  const serialNumber = systemInfo.cb_sn ?? "";

  if (!serialNumber.startsWith(FAKE_PROTO_RIG_SERIAL_PREFIX)) {
    throw new Error(
      `Refusing to ${actionDescription} non-simulator target "${serialNumber || "unknown"}" (${systemInfo.manufacturer ?? "unknown"} ${systemInfo.product_name ?? systemInfo.model ?? "unknown"}).`,
    );
  }
}

export async function waitForAuthenticatedApiRecovery({
  accessToken,
  path,
  request,
  timeoutMs,
}: WaitForAuthenticatedApiRecoveryParams) {
  await expect.poll(() => getAuthenticatedApiStatus({ accessToken, path, request }), { timeout: timeoutMs }).toBe(200);
}

export async function waitForAuthenticatedApiOutage({
  accessToken,
  path,
  request,
  timeoutMs,
}: WaitForAuthenticatedApiRecoveryParams) {
  await expect
    .poll(() => getAuthenticatedApiStatus({ accessToken, path, request }), { timeout: timeoutMs })
    .not.toBe(200);
}
