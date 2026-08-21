import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useSystemTag } from "./useSystemTag";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

const mockGetSystemTag = vi.fn();
const mockPutSystemTag = vi.fn();
const mockAuthRetry = vi.fn();

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useAuthRetry: vi.fn(),
}));

describe("useSystemTag", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    (useMinerHosting as Mock).mockReturnValue({
      api: {
        getSystemTag: mockGetSystemTag,
        putSystemTag: mockPutSystemTag,
      },
    });
    (useAuthRetry as Mock).mockReturnValue(mockAuthRetry);
  });

  test("unwraps the firmware tag response object", async () => {
    mockGetSystemTag.mockResolvedValue({ data: { tag: "PM-H132435034" } });
    const onSuccess = vi.fn();

    const { result } = renderHook(() => useSystemTag());

    result.current.getSystemTag({ onSuccess });

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith("PM-H132435034");
    });
  });

  test("treats an empty firmware tag response object as no miner id", async () => {
    mockGetSystemTag.mockResolvedValue({ data: { tag: "" } });
    const onSuccess = vi.fn();

    const { result } = renderHook(() => useSystemTag());

    result.current.getSystemTag({ onSuccess });

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith("");
    });
  });
});
