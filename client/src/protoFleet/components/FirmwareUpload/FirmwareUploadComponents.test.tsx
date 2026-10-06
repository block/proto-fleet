import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FileDropZone, FileSelectedStatus } from "./FirmwareUploadComponents";

describe("FileDropZone", () => {
  it("opens the file picker with Enter or Space", () => {
    render(<FileDropZone extensions={[".swu"]} onFileSelect={vi.fn()} />);

    const dropZone = screen.getByTestId("firmware-drop-zone");
    const input = screen.getByTestId("firmware-file-input");
    const clickSpy = vi.spyOn(input, "click");

    fireEvent.keyDown(dropZone, { key: "Enter" });
    expect(clickSpy).toHaveBeenCalledTimes(1);

    expect(fireEvent.keyDown(dropZone, { key: " " })).toBe(false);
    expect(clickSpy).toHaveBeenCalledTimes(2);
  });

  it("does not handle keyboard events from the nested choose-file button", () => {
    render(<FileDropZone extensions={[".swu"]} onFileSelect={vi.fn()} />);

    const input = screen.getByTestId("firmware-file-input");
    const clickSpy = vi.spyOn(input, "click");
    fireEvent.keyDown(screen.getByText("Choose file"), { key: "Enter" });

    expect(clickSpy).not.toHaveBeenCalled();
  });

  it("does not accept files while disabled", () => {
    const onFileSelect = vi.fn();
    render(<FileDropZone extensions={[".swu"]} onFileSelect={onFileSelect} disabled />);

    const input = screen.getByTestId("firmware-file-input");
    const clickSpy = vi.spyOn(input, "click");
    expect(input).toBeDisabled();
    const dropZone = screen.getByTestId("firmware-drop-zone");
    expect(dropZone).toHaveAttribute("aria-disabled", "true");

    fireEvent.keyDown(dropZone, { key: "Enter" });
    fireEvent.change(input, { target: { files: [new File(["firmware"], "update.swu")] } });
    expect(clickSpy).not.toHaveBeenCalled();
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
