import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import Toast from "./Toast";
import { STATUSES } from "@/shared/features/toaster";

describe("Toast", () => {
  it("renders an informational clickable toast body", () => {
    const onClick = vi.fn();
    const onClose = vi.fn();

    const { getByRole } = render(
      <Toast message="Update available: Fleet v1.3.0" status={STATUSES.info} onClick={onClick} onClose={onClose} />,
    );

    fireEvent.click(getByRole("button", { name: "Update available: Fleet v1.3.0" }));

    expect(onClick).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });
});
