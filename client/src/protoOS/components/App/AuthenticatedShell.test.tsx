import { render } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import AuthenticatedShell from "./AuthenticatedShell";

const mocks = vi.hoisted(() => ({
  hashboardStatus: vi.fn(),
  checkFirmwareUpdate: vi.fn(() => Promise.resolve()),
}));

vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: mocks.hashboardStatus }));
vi.mock("@/protoOS/api/hooks/useNetworkInfo", () => ({ useNetworkInfo: vi.fn() }));
vi.mock("@/protoOS/api/hooks/useErrors", () => ({ useErrors: vi.fn() }));
vi.mock("@/protoOS/api/hooks/usePoolsInfo", () => ({ usePoolsInfo: vi.fn() }));
vi.mock("@/protoOS/api/hooks/useFirmwareUpdate", () => ({
  useFirmwareUpdate: () => ({ checkFirmwareUpdate: mocks.checkFirmwareUpdate }),
}));

beforeEach(() => vi.clearAllMocks());

test("skips eager ASIC loading", () => {
  render(<AuthenticatedShell reloadSystemInfo={vi.fn()} />);
  expect(mocks.hashboardStatus).not.toHaveBeenCalled();
});
