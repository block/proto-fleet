import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import InfoItem from "./InfoItem";
import LocationInfo from "./LocationInfo";
import { pushToast } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { error: "error", success: "success" },
}));

vi.mock("@/shared/utils/utility", async () => {
  const actual = await vi.importActual<typeof import("@/shared/utils/utility")>("@/shared/utils/utility");
  return { ...actual, copyToClipboard: vi.fn() };
});

const mockCopyToClipboard = vi.mocked(copyToClipboard);
const mockPushToast = vi.mocked(pushToast);

describe("InfoItem", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCopyToClipboard.mockResolvedValue(undefined);
  });

  test("copies the value and confirms with a toast", async () => {
    render(<InfoItem label="IP Address" value="10.0.0.42" />);

    fireEvent.click(screen.getByRole("button", { name: "Copy IP Address 10.0.0.42" }));

    expect(mockCopyToClipboard).toHaveBeenCalledWith("10.0.0.42");
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({ message: "IP Address copied to clipboard", status: "success" }),
    );
  });

  test("reports a failed copy", async () => {
    mockCopyToClipboard.mockRejectedValue(new Error("denied"));
    render(<InfoItem label="IP Address" value="10.0.0.42" />);

    fireEvent.click(screen.getByRole("button", { name: "Copy IP Address 10.0.0.42" }));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({ message: "Failed to copy IP Address", status: "error" }),
    );
  });

  test.each([
    ["the value is loading", { loading: true, value: "10.0.0.42" }],
    ["the value is missing", { value: undefined }],
    ["the value is empty", { value: "" }],
  ])("has no copy button when %s", (_, props) => {
    render(<InfoItem label="IP Address" {...props} />);

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  test("shows the placeholder when the value is missing", () => {
    render(<InfoItem label="IP Address" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });
});

describe("LocationInfo", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCopyToClipboard.mockResolvedValue(undefined);
  });

  test("copies each placement level separately", () => {
    render(<LocationInfo values={["Rockdale", "Building 2", "Rack 14"]} />);

    fireEvent.click(screen.getByRole("button", { name: "Copy Location Building 2" }));

    expect(mockCopyToClipboard).toHaveBeenCalledTimes(1);
    expect(mockCopyToClipboard).toHaveBeenCalledWith("Building 2");
    expect(screen.getAllByRole("button")).toHaveLength(3);
  });

  test("has no copy button when the miner has no placement", () => {
    render(<LocationInfo values={[]} />);

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
