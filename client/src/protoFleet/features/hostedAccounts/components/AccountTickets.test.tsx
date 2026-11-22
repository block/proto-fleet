import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountTickets from "./AccountTickets";
import { AccountSchema, AccountTicketDetailSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import {
  type TicketAsset,
  TicketAssetSchema,
  TicketCategory,
  TicketCommentSchema,
  TicketStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";

const setup = (closed = false, canClose = true, readOnly = false) => {
  const detail = create(AccountTicketDetailSchema, {
    ticket: {
      id: 7n,
      ticketNumber: "TK-0007",
      description: "Equipment stopped",
      notes: "Internal operational notes",
      resolutionNote: closed ? "Resolved client issue" : "",
      status: closed ? TicketStatus.COMPLETED : TicketStatus.OPEN,
      updatedAt: { seconds: 1700000000n, nanos: 123456789 },
    },
    canClose,
  });
  const client = {
    listAccountTickets: vi.fn().mockResolvedValue({ tickets: [detail.ticket], nextBeforeId: 0n }),
    getAccountTicket: vi.fn().mockResolvedValue({ detail }),
    createAccountTicket: vi.fn().mockResolvedValue({ detail }),
    commentAccountTicket: vi.fn().mockResolvedValue({ detail }),
    changeAccountTicketStatus: vi.fn().mockResolvedValue({ detail }),
    listAccountTicketAssets: vi.fn().mockResolvedValue({ assets: [] as TicketAsset[], nextPageToken: "" }),
  };
  const scope: AccountScope = {
    account: create(AccountSchema, { accountId: "client", ticketAccess: true, ticketingEnabled: true }),
    client: client as unknown as AccountScope["client"],
    signal: new AbortController().signal,
    readOnly,
    refreshAccount: vi.fn(),
    invalidateAccount: vi.fn(),
  };
  const view = render(
    <MemoryRouter>
      <AccountScopeContext.Provider value={scope}>
        <AccountTickets />
      </AccountScopeContext.Provider>
    </MemoryRouter>,
  );
  const rerenderScope = (nextScope: AccountScope) =>
    view.rerender(
      <MemoryRouter>
        <AccountScopeContext.Provider value={nextScope}>
          <AccountTickets />
        </AccountScopeContext.Provider>
      </MemoryRouter>,
    );
  return { client, detail, scope, rerenderScope };
};
const conversation = (detail: ReturnType<typeof setup>["detail"]) => {
  const comments = Array.from({ length: 120 }, (_, index) =>
    create(TicketCommentSchema, {
      id: BigInt(index + 1),
      text: `Comment ${index + 1}`,
      userName: "Host",
      createdAt: { seconds: BigInt(index + 1) },
    }),
  );
  const hidden = new Set<bigint>();
  const page = (beforeCommentId = 0n) => {
    const visible = comments.filter(
      (comment) => !hidden.has(comment.id) && (!beforeCommentId || comment.id < beforeCommentId),
    );
    const selected = visible.slice(-50);
    return create(AccountTicketDetailSchema, {
      ...detail,
      comments: selected,
      nextBeforeCommentId: visible.length > 50 ? selected[0].id : 0n,
    });
  };
  const hide = (...ids: bigint[]) => {
    ids.forEach((id) => hidden.add(id));
    detail.ticket!.updatedAt = create(TimestampSchema, {
      ...detail.ticket!.updatedAt!,
      nanos: detail.ticket!.updatedAt!.nanos + 1,
    });
  };
  return { page, hide };
};

describe("Account tickets", () => {
  it("shows the explicit resolution without operational notes", async () => {
    const user = userEvent.setup();
    setup(true);
    await user.click(await screen.findByText("TK-0007"));
    expect(await screen.findByText("Resolved client issue")).toBeInTheDocument();
    expect(screen.queryByText("Internal operational notes")).not.toBeInTheDocument();
  });

  it("submits a description alone without exposing host triage fields", async () => {
    const user = userEvent.setup();
    const { client } = setup();
    await user.click(screen.getByRole("button", { name: "New ticket" }));
    const form = within(screen.getByRole("dialog", { name: "New ticket" }));
    expect(form.queryByLabelText("Assignee")).not.toBeInTheDocument();
    expect(form.queryByLabelText("Category")).not.toBeInTheDocument();
    expect(form.getByRole("button", { name: "Component" })).toHaveTextContent("Unknown");
    expect(form.queryByLabelText("Suspected cause (optional)")).not.toBeInTheDocument();
    expect(form.queryByLabelText("Warranty")).not.toBeInTheDocument();
    await user.type(form.getByLabelText("Description"), "Please check my equipment");
    await user.click(form.getByRole("button", { name: "Create ticket" }));
    await waitFor(() =>
      expect(client.createAccountTicket).toHaveBeenCalledWith(
        expect.objectContaining({
          accountId: "client",
          description: "Please check my equipment",
          component: "Unknown",
          targets: [],
          idempotencyKey: expect.any(String),
        }),
      ),
    );
  });
  it("submits one module and its affected boards using their physical identities", async () => {
    const user = userEvent.setup();
    const { client } = setup();
    client.listAccountTicketAssets.mockResolvedValue({
      nextPageToken: "",
      assets: [
        create(TicketAssetSchema, { kind: TicketCategory.MODULE, id: 51n, siteId: 8n, label: "Module 51" }),
        create(TicketAssetSchema, {
          kind: TicketCategory.HASHBOARD,
          id: 84n,
          moduleId: 51n,
          siteId: 8n,
          label: "HB-84",
        }),
      ],
    });
    await user.click(screen.getByRole("button", { name: "New ticket" }));
    await user.click(screen.getByRole("button", { name: "Select affected asset (optional)" }));
    const picker = within(screen.getByRole("dialog", { name: "Select affected assets" }));
    const checkboxes = await picker.findAllByRole("checkbox");
    await user.click(checkboxes[0]);
    await user.click(checkboxes[1]);
    await user.click(picker.getByRole("button", { name: "Select assets" }));
    const form = within(screen.getByRole("dialog", { name: "New ticket" }));
    await user.click(form.getByRole("button", { name: "Component" }));
    await user.click(screen.getByText("Other", { exact: true }));
    await user.type(form.getByLabelText("Description"), "Module and board stopped reporting");
    await user.click(form.getByRole("button", { name: "Create ticket" }));
    await waitFor(() =>
      expect(client.createAccountTicket).toHaveBeenCalledWith(
        expect.objectContaining({
          component: "Other",
          targets: [
            { kind: 3, id: 51n },
            { kind: 4, id: 84n, parentModuleId: 51n },
          ],
        }),
      ),
    );
  });
  it("requires a resolution note and sends the exact observed ticket version", async () => {
    const user = userEvent.setup();
    const { client, detail } = setup();
    await user.click(await screen.findByText("TK-0007"));
    await user.click(await screen.findByRole("button", { name: "Close ticket" }));
    const form = within(screen.getByRole("dialog", { name: "Close ticket" }));
    expect(form.getByRole("button", { name: "Close ticket" })).toBeDisabled();
    await user.type(form.getByLabelText("Resolution note (visible to host)"), "Resolved after restart");
    await user.click(form.getByRole("button", { name: "Close ticket" }));
    await waitFor(() =>
      expect(client.changeAccountTicketStatus).toHaveBeenCalledWith({
        accountId: "client",
        ticketId: 7n,
        close: true,
        resolutionNote: "Resolved after restart",
        expectedUpdatedAt: detail.ticket!.updatedAt,
      }),
    );
  });
  it("hides close for host-created tickets and allows reopening visible closed tickets", async () => {
    const user = userEvent.setup();
    const { client, detail } = setup(false, false);
    await user.click(await screen.findByText("TK-0007"));
    await screen.findByRole("dialog");
    expect(screen.queryByRole("button", { name: "Close ticket" })).not.toBeInTheDocument();
    detail.ticket!.status = TicketStatus.COMPLETED;
    await user.click(screen.getByRole("button", { name: "Refresh ticket" }));
    await user.click(await screen.findByRole("button", { name: "Reopen ticket" }));
    await user.click(
      within(screen.getByRole("dialog", { name: "Reopen ticket" })).getByRole("button", { name: "Reopen ticket" }),
    );
    await waitFor(() =>
      expect(client.changeAccountTicketStatus).toHaveBeenCalledWith(expect.objectContaining({ close: false })),
    );
  });
  it("starts a new comment operation after a successful post even when text repeats", async () => {
    const user = userEvent.setup();
    const { client } = setup();
    await user.click(await screen.findByText("TK-0007"));
    for (let index = 0; index < 2; index++) {
      await user.type(await screen.findByLabelText("Add a comment"), "Checked again");
      await user.click(screen.getByRole("button", { name: "Post comment" }));
      await waitFor(() => expect(screen.getByLabelText("Add a comment")).toHaveValue(""));
    }
    expect(client.commentAccountTicket).toHaveBeenCalledTimes(2);
    expect(client.commentAccountTicket.mock.calls[1][0].idempotencyKey).not.toBe(
      client.commentAccountTicket.mock.calls[0][0].idempotencyKey,
    );
  });
  it("retries a failed comment post instead of only reloading the ticket", async () => {
    const user = userEvent.setup();
    const { client } = setup();
    client.commentAccountTicket.mockRejectedValueOnce(new ConnectError("Failed to fetch", Code.Unavailable));
    await user.click(await screen.findByText("TK-0007"));
    await user.type(await screen.findByLabelText("Add a comment"), "Checked again");
    await user.click(screen.getByRole("button", { name: "Post comment" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Could not post comment");
    await user.click(within(alert).getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(screen.getByLabelText("Add a comment")).toHaveValue(""));
    expect(client.commentAccountTicket).toHaveBeenCalledTimes(2);
    expect(client.commentAccountTicket.mock.calls[1][0]).toEqual(
      expect.objectContaining({
        text: "Checked again",
        idempotencyKey: client.commentAccountTicket.mock.calls[0][0].idempotencyKey,
      }),
    );
    expect(screen.queryByText("Could not post comment")).not.toBeInTheDocument();
  });
  it("keeps the asset picker request stable when the account workspace polls", async () => {
    const user = userEvent.setup();
    const { client, scope, rerenderScope } = setup();
    await user.click(screen.getByRole("button", { name: "New ticket" }));
    await user.click(screen.getByRole("button", { name: "Select affected asset (optional)" }));
    await screen.findByText("No matching assets.");
    expect(client.listAccountTicketAssets).toHaveBeenCalledTimes(1);

    rerenderScope({ ...scope, account: create(AccountSchema, { ...scope.account, name: "Updated workspace" }) });
    await new Promise((resolve) => setTimeout(resolve, 250));

    expect(client.listAccountTicketAssets).toHaveBeenCalledTimes(1);
    expect(screen.getByText("No matching assets.")).toBeInTheDocument();
  });
  it("prepends older comments and retains the history cursor across refreshes and posts", async () => {
    const user = userEvent.setup();
    const { client, detail } = setup();
    const comment = (id: bigint) => ({
      id,
      text: `Comment ${id}`,
      userName: "Client author",
      createdAt: { seconds: id, nanos: 0 },
    });
    detail.comments = create(AccountTicketDetailSchema, { comments: [comment(5n), comment(6n)] }).comments;
    detail.nextBeforeCommentId = 5n;
    client.getAccountTicket.mockImplementation(async ({ beforeCommentId }) => ({
      detail:
        beforeCommentId === 5n
          ? create(AccountTicketDetailSchema, {
              comments: [comment(3n), comment(4n), comment(5n)],
              nextBeforeCommentId: 3n,
            })
          : beforeCommentId === 3n
            ? create(AccountTicketDetailSchema, { comments: [comment(1n), comment(2n)] })
            : detail,
    }));
    await user.click(await screen.findByText("TK-0007"));
    await user.click(await screen.findByRole("button", { name: "Load older comments" }));
    await screen.findByText("Comment 3");
    expect(screen.getAllByText("Comment 5")).toHaveLength(1);

    detail.comments = create(AccountTicketDetailSchema, { comments: [comment(6n), comment(7n)] }).comments;
    detail.nextBeforeCommentId = 6n;
    await user.click(screen.getByRole("button", { name: "Refresh ticket" }));
    await screen.findByText("Comment 7");
    detail.comments = create(AccountTicketDetailSchema, { comments: [comment(8n), comment(9n)] }).comments;
    detail.nextBeforeCommentId = 8n;
    await user.type(screen.getByLabelText("Add a comment"), "New reply");
    await user.click(screen.getByRole("button", { name: "Post comment" }));
    await waitFor(() => expect(screen.getByLabelText("Add a comment")).toHaveValue(""));
    await user.click(screen.getByRole("button", { name: "Load older comments" }));
    await screen.findByText("Comment 1");

    expect(client.getAccountTicket).toHaveBeenCalledWith(
      expect.objectContaining({ beforeCommentId: 3n }),
      expect.anything(),
    );
    expect(screen.queryByRole("button", { name: "Load older comments" })).not.toBeInTheDocument();
    const feed = screen.getByText("Comment 1").parentElement!.parentElement!;
    expect(
      within(feed)
        .getAllByText(/^Comment \d$/)
        .map((entry) => entry.textContent),
    ).toEqual([
      "Comment 1",
      "Comment 2",
      "Comment 3",
      "Comment 4",
      "Comment 5",
      "Comment 6",
      "Comment 7",
      "Comment 8",
      "Comment 9",
    ]);
  });
  it("removes a deleted or newly internal comment when the latest page refreshes", async () => {
    const user = userEvent.setup();
    const { detail } = setup();
    detail.comments = create(AccountTicketDetailSchema, {
      comments: [{ id: 1n, text: "Previously shared note", userName: "Host" }],
    }).comments;
    await user.click(await screen.findByText("TK-0007"));
    await screen.findByText("Previously shared note");
    detail.comments = [];
    await user.click(screen.getByRole("button", { name: "Refresh ticket" }));
    await waitFor(() => expect(screen.queryByText("Previously shared note")).not.toBeInTheDocument());
  });
  it.each(["polling", "manual refresh"])(
    "discards hidden or deleted non-anchor history after successful %s and preserves drafts",
    async (refresh) => {
      const intervals = vi.spyOn(window, "setInterval");
      try {
        const user = userEvent.setup();
        const { client, detail } = setup();
        const { page, hide } = conversation(detail);
        client.getAccountTicket.mockImplementation(async ({ beforeCommentId }) => ({ detail: page(beforeCommentId) }));
        await user.click(await screen.findByText("TK-0007"));
        await user.click(await screen.findByRole("button", { name: "Load older comments" }));
        await screen.findByText("Comment 35");
        await user.click(screen.getByRole("button", { name: "Load older comments" }));
        await screen.findByText("Comment 1");
        expect(screen.queryByRole("button", { name: "Load older comments" })).not.toBeInTheDocument();
        await act(async () => {
          for (const [callback, interval] of intervals.mock.calls) {
            if (interval === POLL_INTERVAL_MS && typeof callback === "function") callback();
          }
        });
        expect(screen.getByText("Comment 1")).toBeInTheDocument();
        expect(screen.queryByRole("button", { name: "Load older comments" })).not.toBeInTheDocument();
        await user.type(screen.getByLabelText("Add a comment"), "Keep my reply draft");
        await user.click(screen.getByRole("button", { name: "Close ticket" }));
        const closeForm = within(screen.getByRole("dialog", { name: "Close ticket" }));
        await user.type(closeForm.getByLabelText("Resolution note (visible to host)"), "Keep my resolution draft");

        hide(35n);
        if (refresh === "polling") {
          await act(async () => {
            for (const [callback, interval] of intervals.mock.calls) {
              if (interval === POLL_INTERVAL_MS && typeof callback === "function") callback();
            }
          });
        } else {
          await user.click(screen.getByRole("button", { name: "Refresh ticket" }));
        }
        await waitFor(() => expect(screen.queryByText("Comment 35")).not.toBeInTheDocument());
        expect(screen.queryByText("Comment 1")).not.toBeInTheDocument();
        expect(screen.getByText("Comment 120")).toBeInTheDocument();
        expect(screen.getByLabelText("Add a comment")).toHaveValue("Keep my reply draft");
        expect(closeForm.getByLabelText("Resolution note (visible to host)")).toHaveValue("Keep my resolution draft");
        await user.click(screen.getByRole("button", { name: "Load older comments" }));
        await screen.findByText("Comment 20");
        expect(client.getAccountTicket).toHaveBeenLastCalledWith(
          expect.objectContaining({ beforeCommentId: 71n }),
          expect.anything(),
        );
        expect(screen.queryByText("Comment 35")).not.toBeInTheDocument();
      } finally {
        intervals.mockRestore();
      }
    },
  );

  it("ignores an older page that settles after refreshed ticket version invalidates history", async () => {
    const user = userEvent.setup();
    const { client, detail } = setup();
    const { page, hide } = conversation(detail);
    let finishOlder!: () => void;
    let finishLatest!: (value: { detail: typeof detail }) => void;
    let holdLatest = false;
    client.getAccountTicket.mockImplementation(async ({ beforeCommentId }) => {
      if (!beforeCommentId && holdLatest) {
        return new Promise((resolve) => {
          finishLatest = resolve;
        });
      }
      if (beforeCommentId === 21n) {
        const stale = page(beforeCommentId);
        return new Promise((resolve) => {
          finishOlder = () => resolve({ detail: stale });
        });
      }
      return { detail: page(beforeCommentId) };
    });
    await user.click(await screen.findByText("TK-0007"));
    await user.click(await screen.findByRole("button", { name: "Load older comments" }));
    await screen.findByText("Comment 35");
    await user.click(screen.getByRole("button", { name: "Load older comments" }));
    await waitFor(() => expect(finishOlder).toBeDefined());
    hide(5n, 35n);
    holdLatest = true;
    await user.click(screen.getByRole("button", { name: "Refresh ticket" }));
    await waitFor(() => expect(finishLatest).toBeDefined());
    await act(async () => {
      finishLatest({ detail: page() });
      finishOlder();
    });
    expect(screen.queryByText("Comment 35")).not.toBeInTheDocument();
    expect(screen.queryByText("Comment 5")).not.toBeInTheDocument();
    expect(screen.queryByText("Comment 1")).not.toBeInTheDocument();
    expect(screen.getByText("Comment 120")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Load older comments" }));
    await screen.findByText("Comment 20");
    expect(client.getAccountTicket).toHaveBeenLastCalledWith(
      expect.objectContaining({ beforeCommentId: 71n }),
      expect.anything(),
    );
  });

  it("preserves the resolution draft and requires a new confirmation after reviewing a conflict", async () => {
    const user = userEvent.setup();
    const { client, detail } = setup();
    const latest = create(AccountTicketDetailSchema, {
      ...detail,
      ticket: {
        ...detail.ticket!,
        description: "Host updated the issue",
        updatedAt: create(TimestampSchema, { seconds: 1800000000n }),
      },
    });
    client.changeAccountTicketStatus.mockRejectedValueOnce(new ConnectError("Ticket changed", Code.FailedPrecondition));
    await user.click(await screen.findByText("TK-0007"));
    await user.click(await screen.findByRole("button", { name: "Close ticket" }));
    let form = within(screen.getByRole("dialog", { name: "Close ticket" }));
    await user.type(form.getByLabelText("Resolution note (visible to host)"), "My retained resolution");
    await user.click(form.getByRole("button", { name: "Close ticket" }));
    await form.findByText("This ticket changed. Refresh and review it before confirming again.");
    expect(form.getByLabelText("Resolution note (visible to host)")).toHaveValue("My retained resolution");
    expect(form.getByRole("button", { name: "Close ticket" })).toBeDisabled();
    let finishReview!: (value: { detail: typeof latest }) => void;
    client.getAccountTicket.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishReview = resolve;
        }),
    );
    client.getAccountTicket.mockResolvedValue({ detail: latest });
    await user.click(form.getByRole("button", { name: "Refresh and review ticket" }));
    expect(form.getByRole("button", { name: "Close ticket" })).toBeDisabled();
    expect(client.changeAccountTicketStatus).toHaveBeenCalledTimes(1);
    await act(async () => finishReview({ detail: latest }));
    form = within(screen.getByRole("dialog", { name: "Close ticket" }));
    await form.findByText("Host updated the issue");
    expect(form.getByLabelText("Resolution note (visible to host)")).toHaveValue("My retained resolution");
    expect(form.getByRole("button", { name: "Close ticket" })).toBeEnabled();
    expect(client.changeAccountTicketStatus).toHaveBeenCalledTimes(1);
    await user.click(form.getByRole("button", { name: "Close ticket" }));
    await waitFor(() => expect(client.changeAccountTicketStatus).toHaveBeenCalledTimes(2));
    expect(client.changeAccountTicketStatus.mock.calls[1][0]).toEqual(
      expect.objectContaining({
        close: true,
        resolutionNote: "My retained resolution",
        expectedUpdatedAt: latest.ticket!.updatedAt,
      }),
    );
  });
  it("keeps the captured version until the user explicitly reviews a conflict", async () => {
    const user = userEvent.setup();
    const { client, detail } = setup();
    const observed = detail.ticket!.updatedAt;
    await user.click(await screen.findByText("TK-0007"));
    await user.click(await screen.findByRole("button", { name: "Close ticket" }));
    const form = within(screen.getByRole("dialog", { name: "Close ticket" }));
    await user.type(form.getByLabelText("Resolution note (visible to host)"), "Reviewed original ticket");
    client.getAccountTicket.mockResolvedValue({
      detail: create(AccountTicketDetailSchema, {
        ...detail,
        ticket: { ...detail.ticket!, updatedAt: create(TimestampSchema, { seconds: 1800000000n }) },
      }),
    });
    await user.click(screen.getByRole("button", { name: "Refresh ticket" }));
    await waitFor(() => expect(client.getAccountTicket).toHaveBeenCalledTimes(2));
    await user.click(form.getByRole("button", { name: "Close ticket" }));
    expect(client.changeAccountTicketStatus).toHaveBeenCalledWith(
      expect.objectContaining({ expectedUpdatedAt: observed }),
    );
  });
  it.each([
    { closed: false, canClose: false, latestStatus: TicketStatus.OPEN, action: "Close ticket", unavailable: "closed" },
    {
      closed: false,
      canClose: true,
      latestStatus: TicketStatus.COMPLETED,
      action: "Close ticket",
      unavailable: "closed",
    },
    { closed: true, canClose: true, latestStatus: TicketStatus.OPEN, action: "Reopen ticket", unavailable: "reopened" },
  ])(
    "rechecks eligibility before retrying $action",
    async ({ closed, canClose, latestStatus, action, unavailable }) => {
      const user = userEvent.setup();
      const { client, detail } = setup(closed);
      client.changeAccountTicketStatus.mockRejectedValue(new ConnectError("Ticket changed", Code.FailedPrecondition));
      await user.click(await screen.findByText("TK-0007"));
      await user.click(await screen.findByRole("button", { name: action }));
      let form = within(screen.getByRole("dialog", { name: action }));
      if (!closed) await user.type(form.getByLabelText("Resolution note (visible to host)"), "Keep my note");
      await user.click(form.getByRole("button", { name: action }));
      await form.findByText("This ticket changed. Refresh and review it before confirming again.");
      client.getAccountTicket.mockResolvedValue({
        detail: create(AccountTicketDetailSchema, {
          ...detail,
          canClose,
          ticket: {
            ...detail.ticket!,
            status: latestStatus,
            updatedAt: create(TimestampSchema, { seconds: 1800000000n }),
          },
        }),
      });
      await user.click(form.getByRole("button", { name: "Refresh and review ticket" }));
      form = within(screen.getByRole("dialog", { name: action }));
      await form.findByText(`This ticket can no longer be ${unavailable}.`);
      expect(form.getByRole("button", { name: action })).toBeDisabled();
      expect(client.changeAccountTicketStatus).toHaveBeenCalledTimes(1);
      if (!closed) expect(form.getByLabelText("Resolution note (visible to host)")).toHaveValue("Keep my note");
    },
  );
  it("restarts stale comment paging with a fresh cursor and keeps the reply draft", async () => {
    const user = userEvent.setup();
    const { client, detail } = setup();
    const page = (ids: bigint[], nextBeforeCommentId = 0n) =>
      create(AccountTicketDetailSchema, {
        ...detail,
        comments: ids.map((id) =>
          create(TicketCommentSchema, { id, text: `Comment ${id}`, createdAt: { seconds: id } }),
        ),
        nextBeforeCommentId,
      });
    let restarted = false;
    client.getAccountTicket.mockImplementation(async ({ beforeCommentId }) => {
      if (beforeCommentId === 3n) throw new ConnectError("Comment history changed", Code.FailedPrecondition);
      return {
        detail:
          beforeCommentId === 5n
            ? page([3n, 4n], 3n)
            : beforeCommentId === 9n
              ? page([7n, 8n], 7n)
              : restarted
                ? page([9n, 10n], 9n)
                : page([5n, 6n], 5n),
      };
    });
    await user.click(await screen.findByText("TK-0007"));
    await user.click(await screen.findByRole("button", { name: "Load older comments" }));
    await screen.findByText("Comment 3");
    await user.type(screen.getByLabelText("Add a comment"), "Keep my reply");
    await user.click(screen.getByRole("button", { name: "Load older comments" }));
    await screen.findByText("Comment history changed. Refresh the conversation before loading earlier comments.");
    expect(screen.queryByRole("button", { name: "Load older comments" })).not.toBeInTheDocument();
    restarted = true;
    await user.click(screen.getByRole("button", { name: "Refresh conversation" }));
    await screen.findByText("Comment 10");
    expect(screen.queryByText("Comment 3")).not.toBeInTheDocument();
    expect(screen.queryByText("Comment 5")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Add a comment")).toHaveValue("Keep my reply");
    await user.click(screen.getByRole("button", { name: "Load older comments" }));
    await screen.findByText("Comment 7");
    expect(client.getAccountTicket).toHaveBeenLastCalledWith(
      expect.objectContaining({ beforeCommentId: 9n }),
      expect.anything(),
    );
    expect(screen.queryByText("Comment 3")).not.toBeInTheDocument();
  });
  it("shows only a generic label for equipment outside the current site policy", async () => {
    const user = userEvent.setup();
    const { detail } = setup();
    detail.ticket!.targets = create(AccountTicketDetailSchema, {
      ticket: {
        targets: [
          {
            id: 42n,
            outsidePolicy: true,
            label: "Private asset label",
            model: "Private asset model",
            siteId: 99n,
          },
        ],
      },
    }).ticket!.targets;
    await user.click(await screen.findByText("TK-0007"));
    await screen.findByText("Affected equipment outside current site policy");
    expect(screen.queryByText(/Private asset|Private site|ID 42/)).not.toBeInTheDocument();
  });
  it("disables new ticket in preview", async () => {
    setup(false, true, true);
    await screen.findByText("TK-0007");
    expect(screen.getByRole("button", { name: "New ticket" })).toBeDisabled();
  });
});
