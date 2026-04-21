import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeleteFirmwareDialog from "./DeleteFirmwareDialog";

const mockOnConfirm = vi.fn();
const mockOnDismiss = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("DeleteFirmwareDialog", () => {
  it("renders title", () => {
    const { getByText } = render(
      <DeleteFirmwareDialog
        filename="alpha.swu"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText("Delete firmware file?")).toBeInTheDocument();
  });

  it("renders filename in body text", () => {
    const { getByText } = render(
      <DeleteFirmwareDialog
        filename="alpha.swu"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText(/alpha\.swu/)).toBeInTheDocument();
  });

  it("renders warning about irreversibility", () => {
    const { getByText } = render(
      <DeleteFirmwareDialog
        filename="alpha.swu"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText(/This action cannot be undone/)).toBeInTheDocument();
  });

  it("calls onDismiss when Cancel is clicked", () => {
    const { getByText } = render(
      <DeleteFirmwareDialog
        filename="alpha.swu"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    fireEvent.click(getByText("Cancel"));
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("calls onConfirm when Delete is clicked", () => {
    const { getByText } = render(
      <DeleteFirmwareDialog
        filename="alpha.swu"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    fireEvent.click(getByText("Delete"));
    expect(mockOnConfirm).toHaveBeenCalled();
  });

  it("disables Delete button when isSubmitting is true", () => {
    const { getByText } = render(
      <DeleteFirmwareDialog
        filename="alpha.swu"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={true}
      />,
    );

    const deleteButton = getByText("Delete").closest("button");
    expect(deleteButton).toBeDisabled();
  });

  it("disables Cancel button when isSubmitting is true", () => {
    const { getByText } = render(
      <DeleteFirmwareDialog
        filename="alpha.swu"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={true}
      />,
    );

    const cancelButton = getByText("Cancel").closest("button");
    expect(cancelButton).toBeDisabled();
  });
});
