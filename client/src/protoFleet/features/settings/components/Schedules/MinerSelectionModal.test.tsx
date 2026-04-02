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

  it("keeps the global select-all footer enabled for schedule targeting", () => {
    render(<MinerSelectionModal open selectedMinerIds={["miner-1"]} onDismiss={vi.fn()} onSave={vi.fn()} />);

    expect(mockMinerSelectionList).toHaveBeenCalledWith(
      expect.not.objectContaining({
        showSelectAllFooter: false,
      }),
    );
  });
});
