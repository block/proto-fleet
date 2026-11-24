import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { useAssignContainersToSite } from "./useAssignContainersToSite";

const mocks = vi.hoisted(() => ({ assign: vi.fn(), bump: vi.fn(), auth: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ sitesClient: { assignContainersToSite: mocks.assign } }));
vi.mock("@/protoFleet/store", () => ({ useAuthErrors: () => ({ handleAuthErrors: mocks.auth }) }));
vi.mock("@/protoFleet/store/useFleetStore", () => ({
  useFleetStore: (selector: (state: unknown) => unknown) => selector({ ui: { bumpSitesRevision: mocks.bump } }),
}));

beforeEach(() => vi.resetAllMocks());

it("blocks duplicate submissions before rerender and releases the lock for a failed assignment retry", async () => {
  let fail!: (error: unknown) => void;
  mocks.assign.mockImplementationOnce(() => new Promise((_resolve, reject) => (fail = reject)));
  mocks.assign.mockResolvedValue({});
  const updated = vi.fn();
  const { result } = renderHook(() => useAssignContainersToSite(updated));
  let first!: Promise<boolean>;
  let duplicate!: Promise<boolean>;
  act(() => {
    first = result.current.assign(["ct1"], 2n);
    duplicate = result.current.assign(["ct1"], 2n);
  });
  expect(result.current.saving).toBe(true);
  expect(mocks.assign).toHaveBeenCalledOnce();
  await expect(duplicate).resolves.toBe(false);
  const denied = new ConnectError("site:manage required", Code.PermissionDenied);
  await act(async () => {
    fail(denied);
    await expect(first).resolves.toBe(false);
  });
  expect(result.current.saving).toBe(false);
  expect(result.current.error).toContain("You don't have permission to change site assignments.");
  expect(mocks.auth).toHaveBeenCalledWith({ error: denied });
  expect(updated).not.toHaveBeenCalled();
  expect(mocks.bump).not.toHaveBeenCalled();

  await act(async () => expect(await result.current.assign(["ct1"], 2n)).toBe(true));
  expect(result.current.error).toBeUndefined();
  expect(result.current.saving).toBe(false);
  expect(updated).toHaveBeenCalledWith(["ct1"], 2n);
  expect(mocks.bump).toHaveBeenCalledOnce();
  expect(updated.mock.invocationCallOrder[0]).toBeLessThan(mocks.bump.mock.invocationCallOrder[0]);
});
