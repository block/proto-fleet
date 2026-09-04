import { MemoryRouter, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { type FleetNodeItem, useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodeUpgradeIndicator } from "@/protoFleet/components/PageHeader/useFleetNodeUpgradeIndicator";
import { useHasPermission } from "@/protoFleet/store";

vi.mock("@/protoFleet/api/useFleetNodes", () => ({
  useFleetNodes: vi.fn(),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

const node = (upgradeRequired: boolean): FleetNodeItem => ({
  fleetNodeId: "1",
  pendingEnrollmentId: null,
  name: "node-1",
  enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
  identityFingerprint: "abcd1234abcd1234",
  commandProtocolUpgradeRequired: upgradeRequired,
  createdAt: null,
  lastSeenAt: new Date(),
});

const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
};

const Harness = ({ enabled = true }: { enabled?: boolean }) => {
  const pill = useFleetNodeUpgradeIndicator({ enabled });
  const { pathname } = useLocation();
  return (
    <>
      {pill ? (
        <button type="button" onClick={pill.onClick}>
          Upgrade {pill.nodeCount}
        </button>
      ) : null}
      <span data-testid="pathname">{pathname}</span>
    </>
  );
};

describe("useFleetNodeUpgradeIndicator", () => {
  const listFleetNodes = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useHasPermission).mockReturnValue(true);
    listFleetNodes.mockResolvedValue([node(false), node(true)]);
    vi.mocked(useFleetNodes).mockReturnValue({
      listFleetNodes,
      createEnrollmentCode: vi.fn(),
      confirmFleetNode: vi.fn(),
      revokeFleetNode: vi.fn(),
    });
  });

  it("shows the number of incompatible connected nodes and opens node settings", async () => {
    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );

    const button = await screen.findByRole("button", { name: "Upgrade 1" });
    fireEvent.click(button);

    expect(screen.getByTestId("pathname")).toHaveTextContent("/settings/nodes");
  });

  it("stays hidden when every connected node supports current commands", async () => {
    listFleetNodes.mockResolvedValue([node(false)]);

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );

    await waitFor(() => expect(listFleetNodes).toHaveBeenCalledOnce());
    expect(screen.queryByRole("button", { name: /Upgrade/ })).not.toBeInTheDocument();
  });

  it("does not poll without Fleet Node read permission", () => {
    vi.mocked(useHasPermission).mockReturnValue(false);

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );

    expect(listFleetNodes).not.toHaveBeenCalled();
  });

  it("does not poll when disabled", () => {
    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness enabled={false} />
      </MemoryRouter>,
    );

    expect(listFleetNodes).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: /Upgrade/ })).not.toBeInTheDocument();
  });

  it("does not poll or render on the node settings page", () => {
    render(
      <MemoryRouter initialEntries={["/settings/nodes"]}>
        <Harness />
      </MemoryRouter>,
    );

    expect(listFleetNodes).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: /Upgrade/ })).not.toBeInTheDocument();
  });

  it("ignores a response from an earlier polling epoch", async () => {
    const oldRequest = deferred<FleetNodeItem[]>();
    const currentRequest = deferred<FleetNodeItem[]>();
    listFleetNodes.mockReset();
    listFleetNodes
      .mockImplementationOnce(() => oldRequest.promise)
      .mockImplementationOnce(() => currentRequest.promise);

    const { rerender } = render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );
    await waitFor(() => expect(listFleetNodes).toHaveBeenCalledTimes(1));

    rerender(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness enabled={false} />
      </MemoryRouter>,
    );
    rerender(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );
    await waitFor(() => expect(listFleetNodes).toHaveBeenCalledTimes(2));

    await act(async () => currentRequest.resolve([node(false)]));
    await act(async () => oldRequest.resolve([node(true)]));

    expect(screen.queryByRole("button", { name: /Upgrade/ })).not.toBeInTheDocument();
  });

  it("does not revive a cached warning when polling is re-enabled", async () => {
    const currentRequest = deferred<FleetNodeItem[]>();
    listFleetNodes.mockReset();
    listFleetNodes.mockResolvedValueOnce([node(true)]).mockImplementationOnce(() => currentRequest.promise);

    const { rerender } = render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );
    await screen.findByRole("button", { name: "Upgrade 1" });

    rerender(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness enabled={false} />
      </MemoryRouter>,
    );
    rerender(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: /Upgrade/ })).not.toBeInTheDocument();
    await act(async () => currentRequest.resolve([node(false)]));
  });
});
