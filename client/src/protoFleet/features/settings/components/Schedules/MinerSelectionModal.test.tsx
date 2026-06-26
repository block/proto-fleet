import type { ReactNode } from "react";
import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import MinerSelectionModal from "./MinerSelectionModal";

const mockMinerSelectionList = vi.fn();

vi.mock("@/protoFleet/components/MinerSelectionList", () => ({
  __esModule: true,
  default: (props: unknown) => {
    mockMinerSelectionList(props);
    return <div>Miner selection list</div>;
  },
}));

vi.mock("@/shared/components/Modal", () => ({
  __esModule: true,
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

describe("MinerSelectionModal", () => {
  beforeEach(() => {
    mockMinerSelectionList.mockReset();
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
});
