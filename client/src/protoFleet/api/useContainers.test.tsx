import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { ContainerSchema } from "./generated/containers/v1/containers_pb";
import { useContainers } from "./useContainers";

const mocks = vi.hoisted(() => ({ list: vi.fn(), revision: 0 }));
vi.mock("./clients", () => ({ containersClient: { listContainers: mocks.list } }));
vi.mock("@/protoFleet/store/useFleetStore", () => ({
  useFleetStore: (selector: (state: unknown) => unknown) => selector({ ui: { sitesRevision: mocks.revision } }),
}));

const north = create(ContainerSchema, { deviceIdentifier: "north", site: { id: 1n } });
const south = create(ContainerSchema, { deviceIdentifier: "south", site: { id: 2n } });

beforeEach(() => {
  mocks.list.mockReset();
  mocks.revision = 0;
});

describe("useContainers", () => {
  it.each(["refetch", "site revision"])("retains rows while a same-scope %s is pending", async (refresh) => {
    mocks.list.mockResolvedValueOnce({ containers: [north] });
    let finish!: (response: unknown) => void;
    mocks.list.mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)));
    const { result, rerender } = renderHook(() => useContainers([1n], false));
    await waitFor(() => expect(result.current.containers).toEqual([north]));

    if (refresh === "refetch") act(() => result.current.refetch());
    else {
      mocks.revision++;
      rerender();
    }
    expect(mocks.list).toHaveBeenCalledTimes(2);
    expect(result.current.containers).toEqual([north]);
    expect(result.current.loading).toBe(false);
    await act(async () => finish({ containers: [] }));
    expect(result.current.containers).toEqual([]);
  });

  it.each([
    { siteIds: [2n], includeUnassigned: false },
    { siteIds: [1n], includeUnassigned: true },
  ])("hides old rows when the actual scope changes to $siteIds/$includeUnassigned", async (scope) => {
    mocks.list.mockResolvedValueOnce({ containers: [north] });
    let finishOld!: (response: unknown) => void;
    let finishNew!: (response: unknown) => void;
    mocks.list.mockImplementationOnce(() => new Promise((resolve) => (finishOld = resolve)));
    mocks.list.mockImplementationOnce(() => new Promise((resolve) => (finishNew = resolve)));
    const { result, rerender } = renderHook(
      ({ siteIds, includeUnassigned }) => useContainers(siteIds, includeUnassigned),
      { initialProps: { siteIds: [1n], includeUnassigned: false } },
    );
    await waitFor(() => expect(result.current.containers).toEqual([north]));
    act(() => result.current.refetch());
    rerender(scope);
    expect(result.current.containers).toEqual([]);
    expect(result.current.loading).toBe(true);
    await act(async () => finishOld({ containers: [north] }));
    expect(result.current.containers).toEqual([]);
    await act(async () => finishNew({ containers: [south] }));
    expect(result.current.containers).toEqual([south]);
    expect(result.current.loading).toBe(false);
  });
});
