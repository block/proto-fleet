import { act, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import type { TicketDetail } from "../../types";
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

it("defaults to an internal note and requires an explicit client reply", async () => {
  const user = userEvent.setup();
  const onAdd = vi.fn(async (_text: string, _key: string, _scope: { internal: boolean; accountId?: string }) => false);
  render(<TicketComments ticketId="7" comments={[]} canManage sharedWithClients onAdd={onAdd} onDelete={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Add comment" }));
  expect(screen.getByRole("checkbox", { name: "Internal only" })).toBeChecked();
  await user.type(screen.getByLabelText("Add a comment"), "Replaced fan");
  await user.click(screen.getByRole("button", { name: "Post" }));
  expect(onAdd).toHaveBeenLastCalledWith("Replaced fan", expect.any(String), { internal: true });
  const firstKey = onAdd.mock.calls[0][1];
  await user.click(screen.getByRole("checkbox", { name: "Internal only" }));
  await user.click(screen.getByRole("button", { name: "Post" }));
  expect(onAdd).toHaveBeenLastCalledWith("Replaced fan", expect.any(String), { internal: false });
  expect(onAdd.mock.calls[1][1]).not.toBe(firstKey);
});

it("starts a new operation when the same comment text is posted again after success", async () => {
  const user = userEvent.setup();
  const onAdd = vi.fn(async (_text: string, _key: string) => true);
  render(<TicketComments ticketId="7" comments={[]} canManage onAdd={onAdd} onDelete={vi.fn()} />);
  for (let index = 0; index < 2; index++) {
    await user.click(screen.getByRole("button", { name: "Add comment" }));
    await user.type(screen.getByLabelText("Add a comment"), "Checked again");
    await user.click(screen.getByRole("button", { name: "Post" }));
  }
  expect(onAdd).toHaveBeenCalledTimes(2);
  expect(onAdd.mock.calls[1][1]).not.toBe(onAdd.mock.calls[0][1]);
});

const ticketVersion = { $typeName: "google.protobuf.Timestamp" as const, seconds: 1700000000n, nanos: 0 };
const hostNote = {
  id: "9",
  ticketId: "7",
  userId: "2",
  userName: "Host",
  text: "Checked cooling",
  createdAt: null,
  authoredByCaller: true,
  internal: true,
  accountId: "",
  accountName: "",
};
it("requires confirmation before turning off Internal only for a host note", async () => {
  const user = userEvent.setup();
  const onVisibilityChange = vi.fn(async () => true);
  const { rerender } = render(
    <TicketComments
      ticketId="7"
      ticketVersion={ticketVersion}
      canManage
      sharedWithClients
      comments={[hostNote]}
      audiences={[{ accountId: "client", name: "Demo Client" }]}
      onAdd={vi.fn()}
      onDelete={vi.fn()}
      onVisibilityChange={onVisibilityChange}
    />,
  );
  const toggle = screen.getByRole("checkbox", { name: "Internal only" });
  expect(toggle).toBeChecked();
  await user.click(toggle);
  expect(onVisibilityChange).not.toHaveBeenCalled();
  expect(toggle).toBeChecked();
  expect(screen.getByRole("dialog", { name: "Share note with clients?" })).toHaveTextContent("Demo Client");
  expect(screen.queryByRole("button", { name: "Share note with" })).not.toBeInTheDocument();
  rerender(
    <TicketComments
      ticketId="7"
      ticketVersion={{ ...ticketVersion, seconds: ticketVersion.seconds + 1n }}
      canManage
      sharedWithClients
      comments={[hostNote]}
      audiences={[{ accountId: "client", name: "Demo Client" }]}
      onAdd={vi.fn()}
      onDelete={vi.fn()}
      onVisibilityChange={onVisibilityChange}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Share note" }));
  expect(onVisibilityChange).toHaveBeenCalledWith("9", false, ticketVersion, expect.any(Function));
});
it("cancels publication without changing the host note visibility", async () => {
  const user = userEvent.setup();
  const onVisibilityChange = vi.fn();
  render(
    <TicketComments
      ticketId="7"
      ticketVersion={ticketVersion}
      canManage
      sharedWithClients
      comments={[hostNote]}
      onAdd={vi.fn()}
      onDelete={vi.fn()}
      onVisibilityChange={onVisibilityChange}
    />,
  );
  await user.click(screen.getByRole("checkbox", { name: "Internal only" }));
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onVisibilityChange).not.toHaveBeenCalled();
  expect(screen.getByRole("checkbox", { name: "Internal only" })).toBeChecked();
});
it("can make a shared host note internal again without a publication dialog", async () => {
  const user = userEvent.setup();
  const onVisibilityChange = vi.fn(async () => true);
  render(
    <TicketComments
      ticketId="7"
      ticketVersion={ticketVersion}
      canManage
      sharedWithClients
      comments={[{ ...hostNote, internal: false }]}
      onAdd={vi.fn()}
      onDelete={vi.fn()}
      onVisibilityChange={onVisibilityChange}
    />,
  );
  await user.click(screen.getByRole("checkbox", { name: "Internal only" }));
  expect(onVisibilityChange).toHaveBeenCalledWith("9", true, ticketVersion, expect.any(Function));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it("keeps visibility unchanged if publishing fails", async () => {
  const user = userEvent.setup();
  const onVisibilityChange = vi.fn(async () => false);
  render(
    <TicketComments
      ticketId="7"
      ticketVersion={ticketVersion}
      canManage
      sharedWithClients
      comments={[hostNote]}
      onAdd={vi.fn()}
      onDelete={vi.fn()}
      onVisibilityChange={onVisibilityChange}
    />,
  );
  await user.click(screen.getByRole("checkbox", { name: "Internal only" }));
  await user.click(screen.getByRole("button", { name: "Share note" }));
  expect(screen.getByRole("checkbox", { name: "Internal only" })).toBeChecked();
  expect(screen.getByRole("dialog", { name: "Share note with clients?" })).toBeInTheDocument();
});
it("does not offer visibility changes for client-authored replies", () => {
  render(
    <TicketComments
      ticketId="7"
      ticketVersion={ticketVersion}
      canManage
      sharedWithClients
      comments={[{ ...hostNote, internal: false, accountId: "client", accountName: "Demo Client" }]}
      onAdd={vi.fn()}
      onDelete={vi.fn()}
      onVisibilityChange={vi.fn()}
    />,
  );
  expect(screen.queryByRole("checkbox", { name: "Internal only" })).not.toBeInTheDocument();
  expect(screen.getByText("Visible to Demo Client")).toBeInTheDocument();
});

it("cannot post client replies on an internal ticket", async () => {
  const user = userEvent.setup();
  render(<TicketComments ticketId="7" comments={[]} canManage onAdd={vi.fn()} onDelete={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Add comment" }));
  expect(screen.getByRole("checkbox", { name: "Internal only" })).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "Internal only" })).toBeDisabled();
});

it("shows only this share attempt's error inside the confirmation dialog", async () => {
  const user = userEvent.setup();
  const onVisibilityChange = vi.fn(
    async (_id: string, _internal: boolean, _version: typeof ticketVersion, onError?: (message: string) => void) => {
      onError?.("Sharing was denied");
      return false;
    },
  );
  render(
    <TicketComments
      ticketId="7"
      ticketVersion={ticketVersion}
      canManage
      sharedWithClients
      comments={[hostNote]}
      error="An earlier update failed"
      onAdd={vi.fn()}
      onDelete={vi.fn()}
      onVisibilityChange={onVisibilityChange}
    />,
  );
  await user.click(screen.getByRole("checkbox", { name: "Internal only" }));
  const dialog = within(screen.getByRole("dialog", { name: "Share note with clients?" }));
  expect(dialog.queryByRole("alert")).not.toBeInTheDocument();

  await user.click(dialog.getByRole("button", { name: "Share note" }));
  expect(dialog.getByRole("alert")).toHaveTextContent("Sharing was denied");
  await user.click(dialog.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByText("Sharing was denied")).not.toBeInTheDocument();
  await user.click(screen.getByRole("checkbox", { name: "Internal only" }));
  expect(within(screen.getByRole("dialog")).queryByRole("alert")).not.toBeInTheDocument();
});

it("preserves a rejected comment draft and disables mutations during another ticket operation", async () => {
  const user = userEvent.setup();
  const onAdd = vi.fn(async () => false);
  const props = { ticketId: "7", comments: [hostNote], canManage: true, onAdd, onDelete: vi.fn() };
  const { rerender } = render(<TicketComments {...props} />);
  await user.click(screen.getByRole("button", { name: "Add comment" }));
  await user.type(screen.getByLabelText("Add a comment"), "Retain this draft");
  await user.click(screen.getByRole("button", { name: "Post" }));
  expect(screen.getByLabelText("Add a comment")).toHaveValue("Retain this draft");
  rerender(<TicketComments {...props} disabled />);
  expect(screen.getByRole("button", { name: "Post" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Delete comment" })).toBeDisabled();
});

it("requires refreshed publication content to be reviewed before reconfirming with its new version", async () => {
  const user = userEvent.setup();
  const nextVersion = { ...ticketVersion, nanos: 123 };
  const onVisibilityChange = vi.fn(async (_id, _internal, _version, onError) => {
    onError("Ticket changed");
    return false;
  });
  const onRefresh = vi.fn(
    async () =>
      ({
        updatedAtSnapshot: nextVersion,
        comments: [{ ...hostNote, text: "Updated host note" }],
        audiences: [{ accountId: "new-client", name: "Updated Client" }],
      }) as TicketDetail,
  );
  render(
    <TicketComments
      ticketId="7"
      ticketVersion={ticketVersion}
      comments={[hostNote]}
      canManage
      sharedWithClients
      onAdd={vi.fn()}
      onDelete={vi.fn()}
      onVisibilityChange={onVisibilityChange}
      onRefresh={onRefresh}
    />,
  );
  await user.click(screen.getByRole("checkbox", { name: "Internal only" }));
  await user.click(screen.getByRole("button", { name: "Share note" }));
  expect(onVisibilityChange).toHaveBeenLastCalledWith("9", false, ticketVersion, expect.any(Function));
  await user.click(screen.getByRole("button", { name: "Refresh note for review" }));
  expect(onRefresh).toHaveBeenCalledOnce();
  expect(screen.getByText("Updated host note")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Share note" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Use refreshed note" }));
  expect(onVisibilityChange).toHaveBeenCalledOnce();
  await user.click(screen.getByRole("button", { name: "Share note" }));
  expect(onVisibilityChange).toHaveBeenLastCalledWith("9", false, nextVersion, expect.any(Function));
});
