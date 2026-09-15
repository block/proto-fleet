import { act, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import TicketComments from "./TicketComments";

it("opens the comment editor when randomUUID is unavailable", async () => {
  const user = userEvent.setup();
  const originalCrypto = globalThis.crypto;
  vi.stubGlobal("crypto", { getRandomValues: originalCrypto.getRandomValues.bind(originalCrypto) });
  try {
    render(<TicketComments ticketId="7" comments={[]} canManage onAdd={vi.fn()} onDelete={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Add comment" }));

    expect(screen.getByLabelText("Add a comment")).toBeInTheDocument();
  } finally {
    vi.unstubAllGlobals();
  }
});

it("keeps cancellation disabled while a comment submission is pending", async () => {
  let finishRequest!: (ok: boolean) => void;
  const onAdd = vi.fn(
    () =>
      new Promise<boolean>((resolve) => {
        finishRequest = resolve;
      }),
  );
  const user = userEvent.setup();
  render(<TicketComments ticketId="7" comments={[]} canManage onAdd={onAdd} onDelete={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Add comment" }));
  await user.type(screen.getByLabelText("Add a comment"), "Replaced fan");

  await user.click(screen.getByRole("button", { name: "Post" }));

  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  await act(async () => finishRequest(false));
});

it("reuses one comment idempotency key across a failed retry", async () => {
  const user = userEvent.setup();
  const keys: string[] = [];
  const onAdd = vi.fn(async (_text: string, key: string) => {
    keys.push(key);
    return keys.length > 1;
  });

  render(<TicketComments ticketId="7" comments={[]} canManage error={null} onAdd={onAdd} onDelete={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Add comment" }));
  await user.type(screen.getByLabelText("Add a comment"), "Replaced fan");
  await user.click(screen.getByRole("button", { name: "Post" }));
  await user.click(screen.getByRole("button", { name: "Post" }));

  expect(keys).toHaveLength(2);
  expect(keys[0]).toBeTruthy();
  expect(keys[1]).toBe(keys[0]);
});
