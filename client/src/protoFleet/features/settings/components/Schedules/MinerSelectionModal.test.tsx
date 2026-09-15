import { type ReactNode, useEffect } from "react";
import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import MinerSelectionModal from "./MinerSelectionModal";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { MinerSelectionListProps } from "@/protoFleet/components/MinerSelectionList";

const mockMinerSelectionList = vi.fn();
const notifyCount = { current: 0 };
// Bounds the notify loop so a regression fails the assertion instead of
// hanging the run until React gives up.
const NOTIFY_CAP = 100;

// Mirrors the real list's contract: it reports its selection from an effect
// keyed on the callback identity, which is what looped the modal when it
// passed a fresh arrow on every render.
const MinerSelectionListStub = ({
  onSelectionChange,
  initialSelectedItems,
  initialAllSelected,
}: MinerSelectionListProps) => {
  useEffect(() => {
    if (notifyCount.current >= NOTIFY_CAP) {
      return;
    }
    notifyCount.current += 1;
    onSelectionChange?.({
      selectedItems: initialSelectedItems ?? [],
      allSelected: initialAllSelected ?? false,
      totalMiners: 2,
    });
  }, [onSelectionChange, initialSelectedItems, initialAllSelected]);

  return <div>Miner selection list</div>;
};

vi.mock("@/protoFleet/components/MinerSelectionList", () => ({
  __esModule: true,
  default: (props: MinerSelectionListProps) => {
    mockMinerSelectionList(props);
    return <MinerSelectionListStub {...props} />;
  },
}));

vi.mock("@/shared/components/Modal", () => ({
  __esModule: true,
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

const lastListProps = () => {
  const { calls } = mockMinerSelectionList.mock;
  return calls[calls.length - 1]?.[0] as MinerSelectionListProps;
};

describe("MinerSelectionModal", () => {
  beforeEach(() => {
    mockMinerSelectionList.mockReset();
    notifyCount.current = 0;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("disables filtered select-all for schedule targeting", () => {
    render(<MinerSelectionModal open selectedMinerIds={["miner-1"]} onDismiss={vi.fn()} onSave={vi.fn()} />);

    expect(mockMinerSelectionList).toHaveBeenCalledWith(
      expect.objectContaining({
        disableFilteredSelectAll: true,
      }),
    );
  });

  it("forwards the active-site scope to the miner selection list", () => {
    const scope = { siteIds: [7n], includeUnassigned: false };
    render(
      <MinerSelectionModal open selectedMinerIds={["miner-1"]} scope={scope} onDismiss={vi.fn()} onSave={vi.fn()} />,
    );

    expect(mockMinerSelectionList).toHaveBeenCalledWith(expect.objectContaining({ scope }));
  });

  it("keeps the selection-change handler stable across parent re-renders", () => {
    const { rerender } = render(
      <MinerSelectionModal open selectedMinerIds={["miner-1"]} onDismiss={vi.fn()} onSave={vi.fn()} />,
    );
    const firstHandler = mockMinerSelectionList.mock.lastCall?.[0].onSelectionChange;

    rerender(<MinerSelectionModal open selectedMinerIds={["miner-1"]} onDismiss={vi.fn()} onSave={vi.fn()} />);

    expect(mockMinerSelectionList.mock.lastCall?.[0].onSelectionChange).toBe(firstHandler);
  });

  it("forwards drill-down ancestors as the initial miner filter", () => {
    const initialFilter = create(MinerListFilterSchema, { buildingIds: [11n], rackIds: [21n] });
    render(
      <MinerSelectionModal
        open
        selectedMinerIds={[]}
        initialFilter={initialFilter}
        onDismiss={vi.fn()}
        onSave={vi.fn()}
      />,
    );

    expect(mockMinerSelectionList).toHaveBeenCalledWith(expect.objectContaining({ initialFilter }));
  });

  it("keeps onSelectionChange identity stable across re-renders", () => {
    const selectedMinerIds = ["miner-1"];
    const { rerender } = render(
      <MinerSelectionModal open selectedMinerIds={selectedMinerIds} onDismiss={vi.fn()} onSave={vi.fn()} />,
    );
    const initialCallback = lastListProps().onSelectionChange;

    rerender(
      <MinerSelectionModal
        open
        selectedMinerIds={selectedMinerIds}
        scope={{ siteIds: [7n], includeUnassigned: false }}
        onDismiss={vi.fn()}
        onSave={vi.fn()}
      />,
    );

    expect(lastListProps().onSelectionChange).toBe(initialCallback);
  });

  it("does not loop when the list notifies from an effect keyed on the callback", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

    render(<MinerSelectionModal open selectedMinerIds={["miner-1"]} onDismiss={vi.fn()} onSave={vi.fn()} />);

    const loopErrors = consoleError.mock.calls.filter(([message]) =>
      String(message).includes("Maximum update depth exceeded"),
    );
    expect(loopErrors).toEqual([]);
    // The first notification changes the draft (totalMiners becomes known), so
    // the modal re-renders exactly once; the stable callback keeps the list's
    // effect from firing again.
    expect(notifyCount.current).toBe(1);
    expect(mockMinerSelectionList).toHaveBeenCalledTimes(2);
  });

  it("skips draft updates that do not change the selection", () => {
    render(<MinerSelectionModal open selectedMinerIds={["miner-1"]} onDismiss={vi.fn()} onSave={vi.fn()} />);
    const onSelectionChange = lastListProps().onSelectionChange;
    const rendersAfterMount = mockMinerSelectionList.mock.calls.length;

    // Same content as the stub's mount notification, but a fresh array: the
    // updater compares by value and returns the previous state.
    act(() => {
      onSelectionChange?.({ selectedItems: ["miner-1"], allSelected: false, totalMiners: 2 });
    });
    expect(mockMinerSelectionList).toHaveBeenCalledTimes(rendersAfterMount);

    act(() => {
      onSelectionChange?.({ selectedItems: ["miner-1"], allSelected: true, totalMiners: 2 });
    });
    expect(mockMinerSelectionList).toHaveBeenCalledTimes(rendersAfterMount + 1);
  });
});
