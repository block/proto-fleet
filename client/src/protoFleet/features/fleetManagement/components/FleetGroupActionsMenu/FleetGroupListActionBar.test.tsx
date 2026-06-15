import { type ComponentProps } from "react";
import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import { type GroupScope } from "./FleetGroupActionsMenu";
import FleetGroupListActionBar from "./FleetGroupListActionBar";

const { mockSetActionBarVisible } = vi.hoisted(() => ({ mockSetActionBarVisible: vi.fn() }));

vi.mock("@/protoFleet/store", () => ({
  useSetActionBarVisible: () => mockSetActionBarVisible,
}));

// FleetGroupActionsMenu pulls in batch hooks / RPC wiring that aren't relevant
// here — exercise the renderActions lifecycle directly via a stub.
vi.mock("./FleetGroupActionsMenu", async (importOriginal) => {
  const actual = (await importOriginal()) as object;
  return {
    ...actual,
    default: ({ onActionStart, onActionComplete }: { onActionStart?: () => void; onActionComplete?: () => void }) => (
      <div>
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
    mockSetActionBarVisible.mockClear();
    const { getByTestId } = renderBar(scopes(2));

    mockSetActionBarVisible.mockClear();
    fireEvent.click(getByTestId("stub-start"));
    expect(mockSetActionBarVisible).toHaveBeenLastCalledWith(false);

    mockSetActionBarVisible.mockClear();
    fireEvent.click(getByTestId("stub-complete"));
    expect(mockSetActionBarVisible).toHaveBeenLastCalledWith(true);
  });

  test("does not re-show the bar on action complete after unmount", () => {
    const { getByTestId, unmount } = renderBar(scopes(2));

    fireEvent.click(getByTestId("stub-start"));
    const completeButton = getByTestId("stub-complete");

    unmount();
    mockSetActionBarVisible.mockClear();
    fireEvent.click(completeButton);
    expect(mockSetActionBarVisible).not.toHaveBeenCalledWith(true);
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
