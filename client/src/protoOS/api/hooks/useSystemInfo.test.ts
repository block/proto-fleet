import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import { useSystemInfo } from "./useSystemInfo";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { usePoll } from "@/shared/hooks/usePoll";

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useSetSystemInfo: () => vi.fn(),
  useSetSystemInfoError: () => vi.fn(),
  useSetSystemInfoPending: () => vi.fn(),
  useSystemInfo: () => undefined,
  useSystemInfoError: () => undefined,
  useSystemInfoPending: () => false,
}));

vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

describe("useSystemInfo", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (useMinerHosting as Mock).mockReturnValue({ adapter: null });
  });

  test("starts polling when asynchronous adapter resolution completes", () => {
    const adapter = { getSystemInfo: vi.fn() };
    const { rerender } = renderHook(() => useSystemInfo({ poll: true }));

    expect(usePoll).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: false }));

    (useMinerHosting as Mock).mockReturnValue({ adapter });
    rerender();

    expect(usePoll).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: true }));
  });
});
