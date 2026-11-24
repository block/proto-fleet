import { act, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { pushToast } from "../../ToastsObserver";
import Toaster from "../Toaster";
import Toast from "./Toast";

it("renders and invokes a completion toast action", async () => {
  const onClick = vi.fn();
  const close = vi.fn();
  render(
    <Toast
      message="Pool configuration update failed."
      status="error"
      ttl={false}
      onClose={close}
      actions={[{ label: "View details", onClick }]}
    />,
  );
  await userEvent.setup().click(screen.getByRole("button", { name: "View details" }));
  expect(onClick).toHaveBeenCalledOnce();
  expect(close).toHaveBeenCalledOnce();
});

it("preserves actions through the toast container", async () => {
  const onClick = vi.fn();
  render(<Toaster />);
  act(() => {
    pushToast({
      message: "Pool configuration update failed.",
      status: "error",
      ttl: false,
      actions: [{ label: "View details", onClick }],
    });
  });
  await userEvent.setup().click(screen.getByRole("button", { name: "View details" }));
  expect(onClick).toHaveBeenCalledOnce();
});
