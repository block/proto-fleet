import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FileDropZone, FileSelectedStatus } from "./FirmwareUploadComponents";

describe("FileDropZone", () => {
  it("does not accept files while disabled", () => {
    const onFileSelect = vi.fn();
    render(<FileDropZone extensions={[".swu"]} onFileSelect={onFileSelect} disabled />);

    const input = screen.getByTestId("firmware-file-input");
    expect(input).toBeDisabled();
    expect(screen.getByTestId("firmware-drop-zone")).toHaveAttribute("aria-disabled", "true");

    fireEvent.change(input, { target: { files: [new File(["firmware"], "update.swu")] } });
    expect(onFileSelect).not.toHaveBeenCalled();
  });
});

describe("FileSelectedStatus", () => {
  it("shows the selected file and allows it to be removed", () => {
    const onRemove = vi.fn();
    render(<FileSelectedStatus fileName="firmware-2.0.0.swu" fileSize={8} onRemove={onRemove} />);

    expect(screen.getByText("firmware-2.0.0.swu")).toHaveAttribute("title", "firmware-2.0.0.swu");
    fireEvent.click(screen.getByText("Remove"));

    expect(onRemove).toHaveBeenCalledOnce();
  });
});
