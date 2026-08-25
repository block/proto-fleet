import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useSystemTag } from "./useSystemTag";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

const mockGetSystemTag = vi.fn();
const mockPutSystemTag = vi.fn();
const mockAuthRetry = vi.fn();
const mockSetSystemTag = vi.fn();

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useAuthRetry: vi.fn(),
  useSetSystemTag: vi.fn(() => mockSetSystemTag),
  useSystemTagValue: vi.fn(() => undefined),
}));

describe("useSystemTag", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    (useMinerHosting as Mock).mockReturnValue({
      adapter: {
        getSystemTag: mockGetSystemTag,
        setSystemTag: mockPutSystemTag,
      },
    });
    (useAuthRetry as Mock).mockReturnValue(mockAuthRetry);
  });

  test("unwraps the firmware tag response object", async () => {
    mockGetSystemTag.mockResolvedValue("PM-H132435034");
    const onSuccess = vi.fn();

    const { result } = renderHook(() => useSystemTag());

    result.current.getSystemTag({ onSuccess });

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith("PM-H132435034");
      expect(mockSetSystemTag).toHaveBeenCalledWith("PM-H132435034");
    });
  });

  test("treats an empty firmware tag response object as no miner id", async () => {
    mockGetSystemTag.mockResolvedValue("");
    const onSuccess = vi.fn();

    const { result } = renderHook(() => useSystemTag());

    result.current.getSystemTag({ onSuccess });

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith("");
    });
  });
});
