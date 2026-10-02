import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { useContainerOverview } from "./useContainerOverview";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  EntityKind,
  InventoryItemSchema,
  InventoryView,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";

const auth = vi.hoisted(() => ({ logout: vi.fn() }));
vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: ({ error, onError }: { error: unknown; onError: () => void }) => {
      if (error instanceof ConnectError && error.code === Code.Unauthenticated) auth.logout();
      onError();
    },
  }),
}));
const api = vi.hoisted(() => ({ getContainer: vi.fn(), listInventory: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ containersClient: api, fleetInventoryClient: api }));
const container = create(ContainerSchema, {
  id: 7n,
  deviceIdentifier: "remote",
  name: "Remote CT1",
});

beforeEach(() => {
  vi.resetAllMocks();
  api.getContainer.mockResolvedValue({ container });
  api.listInventory.mockResolvedValue({ items: [], nextPageToken: "" });
});
afterEach(() => vi.useRealTimers());

describe("native container loading", () => {
  it("loads all tank/module pages for a FleetNode controller without proxy eligibility", async () => {
    const tank = create(InventoryItemSchema, { ref: { kind: EntityKind.TANK, id: 20n } });
    const module = create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 30n } });
    api.listInventory.mockImplementation(async (request) =>
      request.view === InventoryView.EQUIPMENT_GROUPS
        ? { items: [tank], nextPageToken: "" }
        : request.pageToken
          ? {
              items: [module, create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 31n } })],
              nextPageToken: "",
            }
          : { items: [module], nextPageToken: "second" },
    );
    const { result } = renderHook(() => useContainerOverview("remote"));
    await waitFor(() => expect(result.current.data?.modules).toHaveLength(2));
    expect(result.current.data?.tanks).toEqual([tank]);
    for (const [request, options] of api.listInventory.mock.calls) {
      expect(request.ancestors).toEqual([{ kind: EntityKind.CONTAINER, id: 7n }]);
      expect(options.signal).toBeInstanceOf(AbortSignal);
    }
  });

  it("reports repeated pagination cursors instead of polling indefinitely", async () => {
    api.listInventory.mockResolvedValue({ items: [], nextPageToken: "repeat" });
    const { result } = renderHook(() => useContainerOverview("remote"));
    await waitFor(() => expect(result.current.error).toContain("finish loading"));
    expect(result.current.data).toBeUndefined();
  });

  it("retains the current container through a transient poll failure and recovers", async () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useContainerOverview("remote"));
    await act(async () => {});
    expect(result.current.data?.container).toBe(container);
    api.getContainer.mockRejectedValueOnce(new ConnectError("Offline", Code.Unavailable));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(result.current.data?.container).toBe(container);
    expect(result.current.error).toContain("Offline");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(result.current.error).toBeUndefined();
  });

  it.each([Code.NotFound, Code.PermissionDenied, Code.Unauthenticated])(
    "clears retained data when access or existence changes (%s)",
    async (code) => {
      const { result } = renderHook(() => useContainerOverview("remote"));
      await waitFor(() => expect(result.current.data).toBeDefined());
      api.getContainer.mockRejectedValueOnce(new ConnectError("Unavailable", code));
      act(() => result.current.retry());
      await waitFor(() => expect(result.current.error).toBeDefined());
      expect(result.current.data).toBeUndefined();
      expect(result.current.notFound).toBe(code === Code.NotFound);
    },
  );

  it("aborts old navigation requests and cannot show their late response on the new container", async () => {
    let resolveOld!: (value: { container: typeof container }) => void;
    api.getContainer.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        }),
    );
    const { result, rerender, unmount } = renderHook(({ id }) => useContainerOverview(id), {
      initialProps: { id: "old" },
    });
    const oldSignal = api.getContainer.mock.calls[0][1].signal;
    rerender({ id: "remote" });
    expect(oldSignal.aborted).toBe(true);
    await waitFor(() => expect(result.current.data?.container.name).toBe("Remote CT1"));
    await act(async () => resolveOld({ container: create(ContainerSchema, { id: 8n, name: "Old CT1" }) }));
    expect(result.current.data?.container.name).toBe("Remote CT1");
    const newSignal = api.getContainer.mock.calls[1][1].signal;
    unmount();
    expect(newSignal.aborted).toBe(true);
  });
});

it("logs out when the session expires during initial loading", async () => {
  api.getContainer.mockRejectedValue(new ConnectError("Session expired", Code.Unauthenticated));
  const { result } = renderHook(() => useContainerOverview("remote"));
  await waitFor(() => expect(auth.logout).toHaveBeenCalledOnce());
  expect(result.current.data).toBeUndefined();
});
