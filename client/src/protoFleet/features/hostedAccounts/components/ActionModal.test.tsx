import { act, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import { ActionModal } from "./ActionModal";
import { AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";

const scope = (readOnly: boolean): AccountScope => ({
  account: create(AccountSchema),
  client: {} as AccountScope["client"],
  signal: new AbortController().signal,
  readOnly,
  refreshAccount: vi.fn(),
  invalidateAccount: vi.fn(),
});

describe.each(["modal", "dialog"] as const)("ActionModal (%s)", (presentation) => {
  const mount = ({
    readOnly = false,
    allowDismissWhileBusy = false,
    onSubmit = vi.fn().mockResolvedValue(undefined),
    onDismiss = vi.fn(),
    onTicketingChange = vi.fn(),
    onNameChange = vi.fn(),
  } = {}) =>
    render(
      <AccountScopeContext.Provider value={scope(readOnly)}>
        <ActionModal
          title="Create client"
          presentation={presentation}
          allowDismissWhileBusy={allowDismissWhileBusy}
          onSubmit={onSubmit}
          onDismiss={onDismiss}
        >
          <Input id="client-name" label="Client name" initValue="Client A" onChange={onNameChange} />
          <label>
            <Checkbox checked onChange={onTicketingChange} />
            Ticketing
          </label>
        </ActionModal>
      </AccountScopeContext.Provider>,
    );

  it("locks child controls in read-only scope while allowing dismissal", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    const onDismiss = vi.fn();
    const onTicketingChange = vi.fn();
    const onNameChange = vi.fn();
    mount({ readOnly: true, onSubmit, onDismiss, onTicketingChange, onNameChange });

    const name = screen.getByRole("textbox", { name: "Client name" });
    const ticketing = screen.getByRole("checkbox", { name: "Ticketing" });
    expect(name).toBeDisabled();
    expect(ticketing).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
    await user.type(name, " changed");
    await user.click(ticketing);
    expect(onNameChange).not.toHaveBeenCalled();
    expect(onTicketingChange).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it("locks child controls during a save and restores editing after an error", async () => {
    const user = userEvent.setup();
    let reject!: (error: Error) => void;
    const onSubmit = vi.fn(
      () =>
        new Promise<void>((_resolve, rejectRequest) => {
          reject = rejectRequest;
        }),
    );
    const onDismiss = vi.fn();
    const onTicketingChange = vi.fn();
    const onNameChange = vi.fn();
    mount({ onSubmit, onDismiss, onTicketingChange, onNameChange });

    const name = screen.getByRole("textbox", { name: "Client name" });
    const ticketing = screen.getByRole("checkbox", { name: "Ticketing" });
    expect(name).toBeEnabled();
    expect(ticketing).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(onSubmit).toHaveBeenCalledOnce();
    expect(name).toBeDisabled();
    expect(ticketing).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await user.type(name, " changed");
    await user.click(ticketing);
    await user.keyboard("{Escape}");
    expect(onNameChange).not.toHaveBeenCalled();
    expect(onTicketingChange).not.toHaveBeenCalled();
    expect(onDismiss).not.toHaveBeenCalled();

    await act(async () => reject(new Error("Could not save client")));
    expect(await screen.findByText(/Could not save client/)).toBeInTheDocument();
    expect(name).toBeEnabled();
    expect(ticketing).toBeEnabled();
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
    await user.click(ticketing);
    expect(onTicketingChange).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });
  it.each(["Cancel", "Escape"])("allows opt-in dismissal via %s while keeping form controls locked", async (method) => {
    const user = userEvent.setup();
    let finish!: () => void;
    const onSubmit = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const onDismiss = vi.fn();
    mount({ allowDismissWhileBusy: true, onSubmit, onDismiss });
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByRole("textbox", { name: "Client name" })).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "Ticketing" })).toBeDisabled();
    if (method === "Cancel") await user.click(screen.getByRole("button", { name: "Cancel" }));
    else await user.keyboard("{Escape}");
    expect(onDismiss).toHaveBeenCalledOnce();
    expect(onSubmit).toHaveBeenCalledOnce();
    await act(async () => finish());
  });
});
