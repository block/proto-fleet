import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import LocalFirmwareUpload from "./LocalFirmwareUpload";

const selectFile = (file = new File(["firmware"], "r2.swu")) => {
  fireEvent.change(screen.getByLabelText("Firmware file"), { target: { files: [file] } });
};

describe("LocalFirmwareUpload", () => {
  it("requires an explicit confirmation, then hands off to device progress", async () => {
    const onUpload = vi.fn().mockResolvedValue(true);
    render(<LocalFirmwareUpload pending={false} onUpload={onUpload} />);
    fireEvent.click(screen.getByRole("button", { name: "Upload firmware" }));
    expect(screen.getByRole("button", { name: "Upload and install" })).toBeDisabled();
    const file = new File(["firmware"], "r2.swu");
    selectFile(file);
    await waitFor(() => expect(screen.getByText("r2.swu")).toBeVisible());
    expect(onUpload).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Upload and install" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(onUpload).toHaveBeenCalledWith(file);
  });

  it.each([new File([], "empty.swu"), new File(["x"], "wrong.zip")])("rejects invalid files", (file) => {
    render(<LocalFirmwareUpload pending={false} onUpload={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Upload firmware" }));
    selectFile(file);
    expect(screen.getByRole("alert")).toHaveTextContent("Choose a non-empty .swu");
    expect(screen.getByRole("button", { name: "Upload and install" })).toBeDisabled();
  });

  it("clears a cancelled selection", () => {
    const onUpload = vi.fn();
    render(<LocalFirmwareUpload pending={false} onUpload={onUpload} />);
    fireEvent.click(screen.getByRole("button", { name: "Upload firmware" }));
    selectFile();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("button", { name: "Upload firmware" }));
    expect(screen.queryByText("r2.swu")).not.toBeInTheDocument();
    expect(onUpload).not.toHaveBeenCalled();
  });

  it("clears a previous upload error when a new file is chosen or the dialog closes", () => {
    const onClearError = vi.fn();
    render(
      <LocalFirmwareUpload pending={false} error="Upload failed" onClearError={onClearError} onUpload={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Upload firmware" }));
    expect(onClearError).not.toHaveBeenCalled();
    selectFile();
    expect(onClearError).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClearError).toHaveBeenCalledTimes(2);
  });

  it("locks selection and dismissal while uploading and retains the file on failure", async () => {
    const onUpload = vi.fn().mockResolvedValue(false);
    const { rerender } = render(<LocalFirmwareUpload pending={false} onUpload={onUpload} />);
    fireEvent.click(screen.getByRole("button", { name: "Upload firmware" }));
    selectFile();
    rerender(<LocalFirmwareUpload pending onUpload={onUpload} />);
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Change file" })).toBeDisabled();
    expect(screen.getByRole("status")).toHaveTextContent("Keep this page open");
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    rerender(<LocalFirmwareUpload pending={false} error="Upload failed" onUpload={onUpload} />);
    fireEvent.click(screen.getByRole("button", { name: "Upload and install" }));
    await waitFor(() => expect(onUpload).toHaveBeenCalledOnce());
    expect(screen.getByRole("alert")).toHaveTextContent("Upload failed");
    await waitFor(() => expect(screen.getByText("r2.swu")).toBeVisible());
  });
});
