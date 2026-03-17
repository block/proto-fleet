import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MinerSystemTagEditModal from "./MinerSystemTagEditModal";

const mockPutSystemTag = vi.fn();
vi.mock("@/protoOS/api", () => ({
  useSystemTag: () => ({
    putSystemTag: mockPutSystemTag,
  }),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(() => "toast-id"),
  updateToast: vi.fn(),
  STATUSES: {
    loading: "loading",
    success: "success",
    error: "error",
  },
}));

describe("MinerSystemTagEditModal", () => {
  const defaultProps = {
    open: true,
    currentTag: "",
    onDismiss: vi.fn(),
    onSaved: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders modal with title and description", () => {
    render(<MinerSystemTagEditModal {...defaultProps} />);

    expect(screen.getByText("Proto Rig identification")).toBeInTheDocument();
    expect(screen.getByText("Enter the serial number or asset tag printed on the device label.")).toBeInTheDocument();
  });

  it("renders input with current tag value", () => {
    render(<MinerSystemTagEditModal {...defaultProps} currentTag="PM-H132435034" />);

    const input = screen.getByTestId("miner-id-input");
    expect(input).toHaveValue("PM-H132435034");
  });

  it("disables Save button when input is empty", () => {
    render(<MinerSystemTagEditModal {...defaultProps} />);

    const saveButton = screen.getByText("Save");
    expect(saveButton.closest("button")).toBeDisabled();
  });

  it("calls putSystemTag on save", () => {
    mockPutSystemTag.mockImplementation((_value: string, { onSuccess }: { onSuccess: () => void }) => {
      onSuccess();
    });

    render(<MinerSystemTagEditModal {...defaultProps} currentTag="PM-H132435034" />);

    const saveButton = screen.getByText("Save");
    fireEvent.click(saveButton);

    expect(mockPutSystemTag).toHaveBeenCalledWith("PM-H132435034", expect.any(Object));
  });

  it("calls onSaved with trimmed value after successful save", () => {
    mockPutSystemTag.mockImplementation((_value: string, { onSuccess }: { onSuccess: () => void }) => {
      onSuccess();
    });

    render(<MinerSystemTagEditModal {...defaultProps} currentTag="  PM-H132435034  " />);

    const saveButton = screen.getByText("Save");
    fireEvent.click(saveButton);

    expect(defaultProps.onSaved).toHaveBeenCalledWith("PM-H132435034");
  });

  it("does not render modal content when open is false", () => {
    render(<MinerSystemTagEditModal {...defaultProps} open={false} />);

    expect(screen.queryByText("Proto Rig identification")).not.toBeInTheDocument();
  });
});
