import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import FileSelectedStatus from "./FileSelectedStatus";

describe("FileSelectedStatus", () => {
  it("shows the selected file and allows it to be removed", () => {
    const onRemove = vi.fn();
    render(<FileSelectedStatus fileName="firmware-2.0.0.swu" fileSize={8} onRemove={onRemove} />);

    expect(screen.getByText("firmware-2.0.0.swu")).toHaveAttribute("title", "firmware-2.0.0.swu");
    fireEvent.click(screen.getByText("Remove"));

    expect(onRemove).toHaveBeenCalledOnce();
  });

  it("keeps the file shown but can't be removed while disabled", () => {
    const onRemove = vi.fn();
    render(<FileSelectedStatus fileName="firmware-2.0.0.swu" fileSize={8} onRemove={onRemove} disabled />);

    expect(screen.getByText("firmware-2.0.0.swu")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();
  });
});
