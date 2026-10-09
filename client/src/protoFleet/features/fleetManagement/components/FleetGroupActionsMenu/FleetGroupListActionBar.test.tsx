import { type ComponentProps } from "react";
import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { type GroupScope } from "./FleetGroupActionsMenu";
import FleetGroupListActionBar from "./FleetGroupListActionBar";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const { mockSetActionBarVisible } = vi.hoisted(() => ({ mockSetActionBarVisible: vi.fn() }));

vi.mock("@/protoFleet/store", () => ({
  useSetActionBarVisible: () => mockSetActionBarVisible,
}));
vi.mock("@/protoFleet/features/containers/TankActionsMenu", () => ({
  default: ({
    tanks,
    onActionStart,
    onActionComplete,
  }: {
    tanks: InventoryItem[];
    onActionStart: () => void;
    onActionComplete: () => void;
  }) => (
    <div>
      <span data-testid="stub-tank-names">{tanks.map((tank) => tank.name).join(", ")}</span>
      <button data-testid="stub-tank-start" onClick={onActionStart}>
        start tank action
      </button>
      <button data-testid="stub-tank-complete" onClick={onActionComplete}>
        complete tank action
      </button>
    </div>
  ),
}));

// FleetGroupActionsMenu pulls in batch hooks / RPC wiring that aren't relevant
// here — exercise the renderActions lifecycle directly via a stub.
vi.mock("./FleetGroupActionsMenu", async (importOriginal) => {
  const actual = (await importOriginal()) as object;
  return {
    ...actual,
    default: ({
      scopes,
      onActionStart,
      onActionComplete,
    }: {
      scopes: { id: bigint }[];
      onActionStart?: () => void;
      onActionComplete?: () => void;
    }) => (
      <div>
        <span data-testid="stub-scope-count">{scopes.length}</span>
        <button data-testid="stub-start" onClick={onActionStart}>
          start
        </button>
        <button data-testid="stub-complete" onClick={onActionComplete}>
          complete
        </button>
      </div>
    ),
  };
});

const renderBar = (scopes: GroupScope[], overrides: Partial<ComponentProps<typeof FleetGroupListActionBar>> = {}) =>
  render(
    <FleetGroupListActionBar
      selectedScopes={scopes}
      kind="site"
      onClearSelection={vi.fn()}
      onSelectAllVisible={vi.fn()}
      {...overrides}
    />,
  );

const scopes = (count: number): GroupScope[] =>
  Array.from({ length: count }, (_, i) => ({ kind: "site" as const, id: BigInt(i + 1), name: `Site ${i + 1}` }));

describe("FleetGroupListActionBar", () => {
  test("preserves the confirmed tanks across polling changes without restoring an earlier rack selection", () => {
    const tanks = [create(InventoryItemSchema, { ref: { kind: EntityKind.TANK, id: 1n }, name: "Tank 1" })];
    const props = {
      kind: "rack" as const,
      onClearSelection: vi.fn(),
      onSelectAllVisible: vi.fn(),
      onRefetchTanks: vi.fn(),
    };
    const { getByTestId, queryByTestId, rerender } = render(
      <FleetGroupListActionBar {...props} selectedScopes={[{ kind: "rack", id: 1n, name: "Rack 1" }]} />,
    );
    rerender(<FleetGroupListActionBar {...props} selectedScopes={[]} selectedTanks={tanks} />);
    fireEvent.click(getByTestId("stub-tank-start"));
    rerender(<FleetGroupListActionBar {...props} selectedScopes={[]} selectedTanks={[]} />);
    expect(getByTestId("stub-tank-names")).toHaveTextContent("Tank 1");
    expect(queryByTestId("stub-scope-count")).not.toBeInTheDocument();
    fireEvent.click(getByTestId("stub-tank-complete"));
    expect(queryByTestId("action-bar")).not.toBeInTheDocument();
    expect(mockSetActionBarVisible).toHaveBeenLastCalledWith(false);
  });
  test("sets global toaster push-up on mount and clears it on unmount", () => {
    mockSetActionBarVisible.mockClear();
    const { unmount } = renderBar(scopes(2));

    expect(mockSetActionBarVisible).toHaveBeenCalledWith(true);

    mockSetActionBarVisible.mockClear();
    unmount();
    expect(mockSetActionBarVisible).toHaveBeenLastCalledWith(false);
  });

  test("renders selected count using the kind's plural noun", () => {
    const { getByText } = renderBar(scopes(3));
    expect(getByText("3 sites selected")).toBeInTheDocument();
  });

  test("uses singular noun when exactly one scope is selected", () => {
    const { getByText } = renderBar(scopes(1));
    expect(getByText("1 site selected")).toBeInTheDocument();
  });

  test("hides the bar while an action is running and restores it on complete", () => {
    const onActionBusyChange = vi.fn();
    mockSetActionBarVisible.mockClear();
    const { getByTestId } = renderBar(scopes(2), { onActionBusyChange });

    mockSetActionBarVisible.mockClear();
    fireEvent.click(getByTestId("stub-start"));
    expect(mockSetActionBarVisible).toHaveBeenLastCalledWith(false);
    expect(onActionBusyChange).toHaveBeenLastCalledWith(true);

    mockSetActionBarVisible.mockClear();
    fireEvent.click(getByTestId("stub-complete"));
    expect(mockSetActionBarVisible).toHaveBeenLastCalledWith(true);
    expect(onActionBusyChange).toHaveBeenLastCalledWith(false);
  });

  test("keeps the last selected scopes while an action is running", () => {
    const { getByTestId, queryByTestId, rerender } = renderBar(scopes(2));

    fireEvent.click(getByTestId("stub-start"));
    rerender(
      <FleetGroupListActionBar
        selectedScopes={[]}
        kind="site"
        onClearSelection={vi.fn()}
        onSelectAllVisible={vi.fn()}
      />,
    );
    expect(getByTestId("stub-scope-count")).toHaveTextContent("2");

    fireEvent.click(getByTestId("stub-complete"));
    expect(queryByTestId("stub-scope-count")).not.toBeInTheDocument();
  });

  test("does not re-show the bar on action complete after unmount", () => {
    const onActionBusyChange = vi.fn();
    const { getByTestId, unmount } = renderBar(scopes(2), { onActionBusyChange });

    fireEvent.click(getByTestId("stub-start"));
    const completeButton = getByTestId("stub-complete");

    unmount();
    mockSetActionBarVisible.mockClear();
    fireEvent.click(completeButton);
    expect(mockSetActionBarVisible).not.toHaveBeenCalledWith(true);
    expect(onActionBusyChange).toHaveBeenLastCalledWith(false);
  });

  test("Select all visible fires the callback", () => {
    const onSelectAllVisible = vi.fn();
    const { getByTestId } = renderBar(scopes(1), { onSelectAllVisible });
    fireEvent.click(getByTestId("select-all-visible-sites-button"));
    expect(onSelectAllVisible).toHaveBeenCalledOnce();
  });

  test("Select none fires the clear-selection callback", () => {
    const onClearSelection = vi.fn();
    const { getByTestId } = renderBar(scopes(1), { onClearSelection });
    fireEvent.click(getByTestId("select-none-sites-button"));
    expect(onClearSelection).toHaveBeenCalledOnce();
  });
});
