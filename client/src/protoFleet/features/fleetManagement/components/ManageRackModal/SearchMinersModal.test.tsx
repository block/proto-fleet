import { type ComponentProps, forwardRef, type ReactNode, type Ref, useEffect, useImperativeHandle } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import SearchMinersModal from "./SearchMinersModal";

const mockGetSelection = vi.fn(() => ({
  selectedItems: ["miner-1"],
  allSelected: false,
  totalMiners: 1,
  reassignedItems: [] as string[],
  blockedByFilter: false,
}));

// The real list needs a paginated fleet fetch, and what these tests are about is
// the CTA's behaviour around the caller's write, so the selection is stubbed. The
// stub reports one selected miner on mount, which is what un-gates Assign.
vi.mock("@/protoFleet/components/MinerSelectionList", () => ({
  __esModule: true,
  default: forwardRef(
    ({ onSelectionChange }: { onSelectionChange?: (s: { selectedItems: string[] }) => void }, ref: Ref<unknown>) => {
      useImperativeHandle(ref, () => ({ getSelection: mockGetSelection }));
      useEffect(() => {
        onSelectionChange?.({ selectedItems: ["miner-1"] });
      }, [onSelectionChange]);
      return <div data-testid="miner-selection-list" />;
    },
  ),
}));

vi.mock("@/shared/components/Modal", () => ({
  default: ({
    children,
    open,
    buttons,
  }: {
    children: ReactNode;
    open?: boolean;
    buttons?: { text: string; onClick?: () => void; disabled?: boolean; loading?: boolean }[];
  }) =>
    open !== false ? (
      <div data-testid="modal">
        {children}
        {buttons?.map((btn, i) => (
          <button key={i} onClick={btn.onClick} disabled={Boolean(btn.disabled || btn.loading)}>
            {btn.text}
          </button>
        ))}
      </div>
    ) : null,
}));

const renderModal = (overrides: Partial<ComponentProps<typeof SearchMinersModal>> = {}) =>
  render(
    <SearchMinersModal
      show
      eligibility={{ rackId: 1n }}
      targetRackLabel="Rack 1"
      onDismiss={vi.fn()}
      onConfirm={vi.fn()}
      {...overrides}
    />,
  );

describe("SearchMinersModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("reports the selected miner to the caller on Assign", async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined);
    renderModal({ onConfirm });

    fireEvent.click(screen.getByRole("button", { name: "Assign" }));

    await waitFor(() => expect(onConfirm).toHaveBeenCalledWith("miner-1", false));
  });

  it("blocks a second Assign while the caller's write is in flight", () => {
    // The caller refuses a re-entrant commit and reports it as a failure, which
    // would close this picker and show an error for a write that is still
    // running. So the CTA has to be unavailable for the duration.
    const onConfirm = vi.fn().mockResolvedValue(undefined);
    renderModal({ onConfirm, saving: true });

    const cta = screen.getByRole("button", { name: "Assigning..." });
    expect(cta).toBeDisabled();

    fireEvent.click(cta);
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
