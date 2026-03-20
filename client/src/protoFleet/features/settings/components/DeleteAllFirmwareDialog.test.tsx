import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeleteAllFirmwareDialog from "./DeleteAllFirmwareDialog";

const mockOnConfirm = vi.fn();
const mockOnDismiss = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("DeleteAllFirmwareDialog", () => {
  it("renders title", () => {
    const { getByText } = render(
      <DeleteAllFirmwareDialog
        fileCount={3}
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText("Delete all firmware files?")).toBeInTheDocument();
  });

  it("renders singular text for fileCount=1", () => {
    const { getByText } = render(
      <DeleteAllFirmwareDialog
        fileCount={1}
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText(/1 firmware file/)).toBeInTheDocument();
  });

  it("renders plural text for fileCount > 1", () => {
    const { getByText } = render(
      <DeleteAllFirmwareDialog
        fileCount={5}
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText(/all 5 firmware files/)).toBeInTheDocument();
  });

  it("renders warning about irreversibility", () => {
    const { getByText } = render(
      <DeleteAllFirmwareDialog
        fileCount={2}
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText(/This action cannot be undone/)).toBeInTheDocument();
  });

  it("calls onDismiss when Cancel is clicked", () => {
    const { getByText } = render(
      <DeleteAllFirmwareDialog
        fileCount={2}
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    fireEvent.click(getByText("Cancel"));
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("calls onConfirm when Delete all is clicked", () => {
    const { getByText } = render(
      <DeleteAllFirmwareDialog
        fileCount={2}
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    fireEvent.click(getByText("Delete all"));
    expect(mockOnConfirm).toHaveBeenCalled();
  });

  it("disables Delete all button when isSubmitting is true", () => {
    const { getByText } = render(
      <DeleteAllFirmwareDialog fileCount={2} onConfirm={mockOnConfirm} onDismiss={mockOnDismiss} isSubmitting={true} />,
    );

    const deleteButton = getByText("Delete all").closest("button");
    expect(deleteButton).toBeDisabled();
  });
});
