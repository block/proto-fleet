import type { APIRequestContext } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { expect } from "../fixtures/pageFixtures";

export function generateStrongPassword() {
  const randomSuffix = Math.random().toString(36).slice(2, 10);
  return `ProtoOS!${randomSuffix}9A`;
}

export async function loginViaApi(request: APIRequestContext, password: string) {
  const response = await request.post("/api/v1/auth/login", {
    data: { password },
  });

  expect(response.status()).toBe(200);

  const responseBody = (await response.json()) as { access_token?: string };
  const accessToken = responseBody.access_token;

  if (!accessToken) {
    throw new Error("Missing access token in login response");
  }

  return accessToken;
}

export async function restoreAdminPassword(request: APIRequestContext, currentPassword: string) {
  const accessToken = await loginViaApi(request, currentPassword);
  const response = await request.put("/api/v1/auth/change-password", {
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
    data: {
      current_password: currentPassword,
      new_password: testConfig.admin.password,
    },
  });

  expect(response.status()).toBe(200);
}
