import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FileDropZone } from "./FirmwareUploadComponents";

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
