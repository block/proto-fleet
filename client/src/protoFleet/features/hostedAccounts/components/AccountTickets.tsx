import { useCallback, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Code, ConnectError } from "@connectrpc/connect";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { useAccountMutation } from "../useAccountMutation";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { PageControls } from "./PageControls";
import type { AccountTicketDetail } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import {
  type RepairTicket,
  type TicketAsset,
  TicketStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import TicketAssetPicker from "@/protoFleet/features/maintenance/components/TicketAssetPicker";
import { useTicketOperationKey } from "@/protoFleet/features/maintenance/idempotency";
import { toTicketStatus } from "@/protoFleet/features/maintenance/mappers";
import { ticketAssetKey, ticketAssetLabel, ticketAssetSelection } from "@/protoFleet/features/maintenance/ticketAssets";
import { useFleetStore } from "@/protoFleet/store";
import Button, { sizes } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

const statusLabel = (ticket: RepairTicket) =>
  ticket.status === TicketStatus.COMPLETED ? "Closed" : toTicketStatus(ticket.status).replaceAll("_", " ");

const mergeComments = (...pages: AccountTicketDetail["comments"][]) =>
  [...new Map(pages.flat().map((comment) => [comment.id, comment])).values()].sort((left, right) => {
    const seconds = (left.createdAt?.seconds ?? 0n) - (right.createdAt?.seconds ?? 0n);
    if (seconds) return seconds < 0n ? -1 : 1;
    const nanos = (left.createdAt?.nanos ?? 0) - (right.createdAt?.nanos ?? 0);
    if (nanos) return nanos;
    return left.id < right.id ? -1 : left.id > right.id ? 1 : 0;
  });

const reconcileLatestComments = (previous: AccountTicketDetail["comments"], detail: AccountTicketDetail) => {
  if (!detail.nextBeforeCommentId) return detail.comments;
  const oldest = detail.comments[0];
  if (!oldest) return detail.comments;
  const ordered = mergeComments(previous, detail.comments);
  const boundary = ordered.findIndex((comment) => comment.id === oldest.id);
  return mergeComments(ordered.slice(0, boundary), detail.comments);
};

const NewAccountTicket = ({
  onDismiss,
  onCreated,
}: {
  onDismiss: () => void;
  onCreated: (detail: AccountTicketDetail) => void;
}) => {
  const { account, client, invalidateAccount, signal: accountSignal } = useRequiredAccountScope();
  const [description, setDescription] = useState("");
  const [component, setComponent] = useState("");
  const [asset, setAsset] = useState<TicketAsset | null>(null);
  const [picking, setPicking] = useState(false);
  const { keyFor: operationKey } = useTicketOperationKey();
  const loadAssets = useCallback(
    async (search: string, signal: AbortSignal) => {
      const generation = useFleetStore.getState().auth.sessionGeneration;
      try {
        const response = await client.listAccountTicketAssets(
          { accountId: account.accountId, search },
          { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
        );
        return response.assets;
      } catch (cause) {
        if (!signal.aborted && !accountSignal.aborted) {
          if (ConnectError.from(cause).code === Code.PermissionDenied) invalidateAccount();
          if (
            ConnectError.from(cause).code === Code.Unauthenticated &&
            useFleetStore.getState().auth.sessionGeneration === generation
          )
            useFleetStore.getState().auth.logout();
        }
        throw cause;
      }
    },
    [account.accountId, client, invalidateAccount, accountSignal],
  );
  return (
    <>
      <ActionModal
        title="New ticket"
        confirm="Create ticket"
        valid={!!description.trim()}
        onDismiss={onDismiss}
        onSubmit={async () => {
          const payload = {
            accountId: account.accountId,
            description: description.trim(),
            component: component.trim(),
            targets: asset ? [ticketAssetSelection(asset)] : [],
          };
          const response = await client.createAccountTicket({ ...payload, idempotencyKey: operationKey(payload) });
          if (!response.detail) throw new Error("Ticket could not be loaded. Refresh the ticket list before retrying.");
          onCreated(response.detail);
        }}
      >
        <Textarea
          id="account-ticket-description"
          label="Description"
          maxLength={4096}
          required
          initValue={description}
          onChange={setDescription}
          rows={4}
        />
        <Input
          id="account-ticket-component"
          label="Component (optional)"
          maxLength={255}
          initValue={component}
          onChange={setComponent}
        />
        {asset ? (
          <div className="flex flex-wrap items-center gap-3">
            <span>{ticketAssetLabel(asset)}</span>
            <Button text="Clear asset" variant="textOnly" onClick={() => setAsset(null)} />
          </div>
        ) : null}
        <Button
          text="Select affected asset (optional)"
          variant="secondary"
          size={sizes.compact}
          onClick={() => setPicking(true)}
        />
      </ActionModal>
      {picking ? (
        <TicketAssetPicker
          multiple={false}
          load={loadAssets}
          onDismiss={() => setPicking(false)}
          onSelect={(assets) => {
            setAsset(assets[0]);
            setPicking(false);
          }}
        />
      ) : null}
    </>
  );
};

const AccountTicketView = ({
  ticketId,
  onDismiss,
  onChanged,
}: {
  ticketId: bigint;
  onDismiss: () => void;
  onChanged: () => void;
}) => {
  const { account, client, readOnly, signal: accountSignal } = useRequiredAccountScope();
  const [retainedComments, setRetainedComments] = useState<AccountTicketDetail["comments"]>([]);
  const [beforeCommentId, setBeforeCommentId] = useState(0n);
  const ticketVersion = useRef<string | null>(null);
  const conversationGeneration = useRef(0);
  const load = useCallback(
    async (signal: AbortSignal) => {
      const response = await client.getAccountTicket(
        { accountId: account.accountId, ticketId },
        { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
      );
      if (!signal.aborted && response.detail) {
        const updatedAt = response.detail.ticket?.updatedAt;
        const version = updatedAt ? `${updatedAt.seconds}:${updatedAt.nanos}` : "";
        const changed = ticketVersion.current !== null && ticketVersion.current !== version;
        ticketVersion.current = version;
        if (changed) {
          conversationGeneration.current++;
          setBeforeCommentId(0n);
        }
        setRetainedComments((comments) =>
          changed ? response.detail!.comments : reconcileLatestComments(comments, response.detail!),
        );
      }
      return response;
    },
    [account.accountId, client, ticketId],
  );
  const query = useAccountQuery(load, true, true, POLL_INTERVAL_MS);
  const mutation = useAccountMutation();
  const reviewMutation = useAccountMutation();
  const detail = query.data?.detail;
  const ticket = detail?.ticket;
  const loadOlder = useCallback(
    async (signal: AbortSignal) => {
      const generation = conversationGeneration.current;
      const response = await client.getAccountTicket(
        { accountId: account.accountId, ticketId, beforeCommentId },
        { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
      );
      if (!signal.aborted && generation === conversationGeneration.current && response.detail)
        setRetainedComments((comments) => mergeComments(comments, response.detail!.comments));
      return response;
    },
    [account.accountId, beforeCommentId, client, ticketId],
  );
  const olderQuery = useAccountQuery(loadOlder, beforeCommentId > 0n, true);
  const comments = retainedComments;
  const nextBeforeCommentId =
    olderQuery.data?.detail?.nextBeforeCommentId ?? (beforeCommentId || detail?.nextBeforeCommentId || 0n);
  const [comment, setComment] = useState("");
  const [statusAction, setStatusAction] = useState<{
    ticket: RepairTicket;
    close: boolean;
    needsReview: boolean;
    eligible: boolean;
    review: number;
  } | null>(null);
  const [resolutionNote, setResolutionNote] = useState("");
  const { keyFor: operationKey, reset: resetCommentOperation } = useTicketOperationKey();
  const refreshed = () => {
    query.refresh();
    onChanged();
  };
  const restartConversation = () => {
    conversationGeneration.current++;
    setRetainedComments([]);
    setBeforeCommentId(0n);
    olderQuery.invalidate();
    query.invalidate();
  };
  const staleConversation = olderQuery.errorCode === Code.FailedPrecondition;
  const closed = ticket?.status === TicketStatus.COMPLETED;
  return (
    <>
      <Modal
        open
        manageFocus
        dialogLabel={ticket?.ticketNumber ?? "Ticket"}
        title={ticket?.ticketNumber ?? "Ticket"}
        onDismiss={mutation.busy ? undefined : onDismiss}
        buttons={[
          {
            text: "Refresh ticket",
            variant: "secondary",
            disabled: mutation.busy,
            dismissModalOnClick: false,
            onClick: query.refresh,
          },
          ...(!readOnly && ticket && (closed || detail?.canClose)
            ? [
                {
                  text: closed ? "Reopen ticket" : "Close ticket",
                  variant: "primary" as const,
                  disabled: mutation.busy,
                  dismissModalOnClick: false,
                  onClick: () => {
                    setResolutionNote("");
                    reviewMutation.clearError();
                    setStatusAction({ ticket, close: !closed, needsReview: false, eligible: true, review: 0 });
                  },
                },
              ]
            : []),
        ]}
      >
        <div className="flex flex-col gap-5">
          <AccountState loading={query.loading} error={query.error || mutation.error} retry={query.refresh} />
          {ticket ? (
            <>
              <p className="text-300 capitalize">{statusLabel(ticket)}</p>
              <p className="whitespace-pre-wrap">{ticket.description || ticket.component}</p>
              {ticket.component ? <p>Component: {ticket.component}</p> : null}
              {ticket.targets.map((asset) => (
                <p key={ticketAssetKey(asset)}>
                  {asset.outsidePolicy ? "Affected equipment outside current site policy" : ticketAssetLabel(asset)}
                </p>
              ))}
              {ticket.resolutionNote ? (
                <div>
                  <h3 className="text-emphasis-300">Resolution note</h3>
                  <p className="whitespace-pre-wrap">{ticket.resolutionNote}</p>
                </div>
              ) : null}
              <div className="flex flex-col gap-4">
                <h3 className="text-heading-100">Conversation</h3>
                {nextBeforeCommentId > 0n && !staleConversation ? (
                  <Button
                    className="self-start"
                    text="Load older comments"
                    variant="secondary"
                    size={sizes.compact}
                    loading={olderQuery.loading}
                    onClick={() => {
                      if (beforeCommentId === nextBeforeCommentId) olderQuery.refresh();
                      else setBeforeCommentId(nextBeforeCommentId);
                    }}
                  />
                ) : null}
                {staleConversation ? (
                  <div className="flex flex-col items-start gap-3" role="alert">
                    <p>Comment history changed. Refresh the conversation before loading earlier comments.</p>
                    <Button
                      text="Refresh conversation"
                      variant="secondary"
                      size={sizes.compact}
                      onClick={restartConversation}
                    />
                  </div>
                ) : (
                  <AccountState loading={false} error={olderQuery.error} retry={olderQuery.refresh} />
                )}
                <div className="flex flex-col divide-y divide-border-5">
                  {comments.map((entry) => (
                    <div key={String(entry.id)} className="flex flex-col gap-1 py-3 first:pt-0 last:pb-0">
                      <p className="text-200 text-text-primary-70">
                        {entry.userName}
                        {entry.createdAt
                          ? ` · ${new Date(Number(entry.createdAt.seconds) * 1000).toLocaleString()}`
                          : ""}
                      </p>
                      <p className="wrap-anywhere whitespace-pre-wrap">{entry.text}</p>
                    </div>
                  ))}
                </div>
                {!readOnly ? (
                  <>
                    <Textarea
                      id="account-ticket-comment"
                      label="Add a comment"
                      initValue={comment}
                      maxLength={4096}
                      rows={3}
                      disabled={mutation.busy}
                      onChange={setComment}
                    />
                    <Button
                      text="Post comment"
                      variant="primary"
                      size={sizes.compact}
                      disabled={!comment.trim()}
                      loading={mutation.busy}
                      onClick={() =>
                        void mutation
                          .run(async () => {
                            const payload = { accountId: account.accountId, ticketId, text: comment.trim() };
                            await client.commentAccountTicket({ ...payload, idempotencyKey: operationKey(payload) });
                          })
                          .then((ok) => {
                            if (ok) {
                              resetCommentOperation();
                              setComment("");
                              refreshed();
                            }
                          })
                      }
                    />
                  </>
                ) : null}
              </div>
            </>
          ) : null}
        </div>
      </Modal>
      {statusAction ? (
        <ActionModal
          key={statusAction.review}
          title={statusAction.close ? "Close ticket" : "Reopen ticket"}
          confirm={statusAction.close ? "Close ticket" : "Reopen ticket"}
          valid={Boolean(
            statusAction.ticket.updatedAt &&
            !statusAction.needsReview &&
            statusAction.eligible &&
            !reviewMutation.busy &&
            (!statusAction.close || resolutionNote.trim()),
          )}
          onDismiss={() => setStatusAction(null)}
          onSubmit={async () => {
            try {
              await client.changeAccountTicketStatus({
                accountId: account.accountId,
                ticketId,
                close: statusAction.close,
                resolutionNote: resolutionNote.trim(),
                expectedUpdatedAt: statusAction.ticket.updatedAt,
              });
            } catch (cause) {
              if ([Code.Aborted, Code.FailedPrecondition].includes(ConnectError.from(cause).code))
                setStatusAction((action) => (action ? { ...action, needsReview: true } : null));
              throw cause;
            }
            setStatusAction(null);
            refreshed();
          }}
        >
          {statusAction.needsReview || !statusAction.eligible ? (
            <div className="flex flex-col items-start gap-3" role="alert">
              <p>
                {statusAction.needsReview
                  ? "This ticket changed. Refresh and review it before confirming again."
                  : `This ticket can no longer be ${statusAction.close ? "closed" : "reopened"}.`}
              </p>
              <Button
                text="Refresh and review ticket"
                variant="secondary"
                size={sizes.compact}
                loading={reviewMutation.busy}
                onClick={() =>
                  void reviewMutation.run(async () => {
                    const response = await client.getAccountTicket(
                      { accountId: account.accountId, ticketId },
                      { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal: accountSignal },
                    );
                    if (accountSignal.aborted) return;
                    const latest = response.detail?.ticket;
                    if (!latest) throw new Error("Ticket could not be loaded. Refresh and review it before retrying.");
                    setStatusAction((action) =>
                      action === statusAction
                        ? {
                            ...action,
                            ticket: latest,
                            needsReview: false,
                            eligible: action.close
                              ? Boolean(response.detail?.canClose) && latest.status !== TicketStatus.COMPLETED
                              : latest.status === TicketStatus.COMPLETED,
                            review: action.review + 1,
                          }
                        : action,
                    );
                    query.refresh();
                  })
                }
              />
              {reviewMutation.error ? <p role="alert">{reviewMutation.error}</p> : null}
            </div>
          ) : null}
          {statusAction.review > 0 ? (
            <div className="flex flex-col gap-2">
              <p className="capitalize">Current status: {statusLabel(statusAction.ticket)}</p>
              <p className="whitespace-pre-wrap">{statusAction.ticket.description || statusAction.ticket.component}</p>
            </div>
          ) : null}
          {!statusAction.close ? (
            <p>Reopen {statusAction.ticket.ticketNumber}?</p>
          ) : (
            <Textarea
              id="account-ticket-resolution"
              label="Resolution note (visible to host)"
              maxLength={4096}
              required
              initValue={resolutionNote}
              onChange={setResolutionNote}
              rows={3}
            />
          )}
        </ActionModal>
      ) : null}
    </>
  );
};

const AccountTickets = () => {
  const { account, client, readOnly } = useRequiredAccountScope();
  const [params, setParams] = useSearchParams();
  const [creating, setCreating] = useState(false);
  const [status, setStatus] = useState("all");
  const [cursors, setCursors] = useState([0n]);
  const [page, setPage] = useState(0);
  const enabled = account.ticketingEnabled && account.ticketAccess;
  const load = useCallback(
    (signal: AbortSignal) =>
      client.listAccountTickets(
        {
          accountId: account.accountId,
          beforeId: cursors[page],
          statuses:
            status === "closed"
              ? [TicketStatus.COMPLETED]
              : status === "open"
                ? [TicketStatus.OPEN, TicketStatus.IN_PROGRESS, TicketStatus.ON_HOLD, TicketStatus.SENT_TO_VENDOR]
                : [],
        },
        { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
      ),
    [account.accountId, client, cursors, page, status],
  );
  const query = useAccountQuery(load, enabled, true, POLL_INTERVAL_MS);
  const selected = params.get("ticket");
  const ticketId = selected && /^[1-9]\d*$/.test(selected) ? BigInt(selected) : null;
  const selectTicket = (id: bigint | null) => {
    const next = new URLSearchParams(params);
    if (id) next.set("ticket", String(id));
    else next.delete("ticket");
    setParams(next);
  };
  if (!enabled) return null;
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-heading-300">Tickets</h1>
        <Button
          text="New ticket"
          variant="primary"
          size={sizes.compact}
          disabled={readOnly}
          onClick={() => setCreating(true)}
        />
      </div>
      <Select
        id="account-ticket-status"
        label="Status"
        variant="filter"
        value={status}
        options={[
          { value: "all", label: "All tickets" },
          { value: "open", label: "Open tickets" },
          { value: "closed", label: "Closed tickets" },
        ]}
        onChange={(value) => {
          setStatus(value);
          setPage(0);
          setCursors([0n]);
        }}
      />
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {query.data ? (
        <>
          <List<RepairTicket, bigint, "ticket" | "description" | "status">
            items={query.data.tickets}
            itemKey="id"
            itemSelectable={false}
            stickyFirstColumn={false}
            activeCols={["ticket", "description", "status"]}
            colTitles={{ ticket: "Ticket", description: "Issue", status: "Status" }}
            colConfig={{
              ticket: {
                width: "min-w-28",
                component: (ticket) => (
                  <Button text={ticket.ticketNumber} variant="textOnly" onClick={() => selectTicket(ticket.id)} />
                ),
              },
              description: {
                width: "min-w-64",
                allowWrap: true,
                component: (ticket) => ticket.description || ticket.component,
              },
              status: {
                width: "min-w-32",
                component: (ticket) => <span className="capitalize">{statusLabel(ticket)}</span>,
              },
            }}
            noDataElement={<p>No tickets.</p>}
          />
          <PageControls
            previous={page > 0}
            next={query.data.nextBeforeId > 0n}
            onPrevious={() => setPage((value) => value - 1)}
            onNext={() => {
              setCursors((items) => [...items.slice(0, page + 1), query.data!.nextBeforeId]);
              setPage((value) => value + 1);
            }}
          />
        </>
      ) : null}
      {creating ? (
        <NewAccountTicket
          onDismiss={() => setCreating(false)}
          onCreated={(detail) => {
            setCreating(false);
            query.invalidate();
            if (detail.ticket) selectTicket(detail.ticket.id);
          }}
        />
      ) : null}
      {ticketId ? (
        <AccountTicketView
          key={String(ticketId)}
          ticketId={ticketId}
          onDismiss={() => selectTicket(null)}
          onChanged={query.refresh}
        />
      ) : null}
    </div>
  );
};
export default AccountTickets;
