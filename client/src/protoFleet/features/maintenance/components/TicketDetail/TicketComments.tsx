import { useState } from "react";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { useTicketOperationKey } from "../../idempotency";
import type { TicketCommentItem, TicketDetail } from "../../types";
import { Trash } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Switch from "@/shared/components/Switch";
import Textarea from "@/shared/components/Textarea";

interface TicketCommentsProps {
  ticketId: string;
  ticketVersion?: Timestamp | null;
  comments: TicketCommentItem[];
  canManage: boolean;
  disabled?: boolean;
  onRefresh?: () => Promise<TicketDetail | null>;
  sharedWithClients?: boolean;
  onVisibilityChange?: (
    id: string,
    internal: boolean,
    version: Timestamp,
    onError?: (message: string) => void,
  ) => Promise<boolean>;
  error?: string | null;
  audiences?: { accountId: string; name: string }[];
  onAdd: (text: string, idempotencyKey: string, audience: { internal: boolean }) => Promise<boolean>;
  onDelete: (id: string) => Promise<boolean>;
}
const TicketComments = ({
  ticketId,
  ticketVersion,
  comments,
  canManage,
  disabled = false,
  onRefresh,
  sharedWithClients = false,
  onVisibilityChange,
  error,
  audiences = [],
  onAdd,
  onDelete,
}: TicketCommentsProps) => {
  const [text, setText] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState(false);
  const { keyFor: operationKey, reset: resetOperation } = useTicketOperationKey();
  const [internal, setInternal] = useState(true);
  const [visibilityError, setVisibilityError] = useState<string | null>(null);
  type PublicationPreview = { comment: TicketCommentItem; version: Timestamp; audiences: typeof audiences };
  const [sharingNote, setSharingNote] = useState<PublicationPreview | null>(null);
  const [noteReview, setNoteReview] = useState<PublicationPreview | null>(null);
  const blocked = busy || disabled;
  const cancelSharing = () => {
    setSharingNote(null);
    setNoteReview(null);
    setVisibilityError(null);
  };
  const refreshNote = async () => {
    if (!onRefresh || !sharingNote || blocked) return;
    setBusy(true);
    try {
      const latest = await onRefresh();
      const comment = latest?.comments.find((item) => item.id === sharingNote.comment.id);
      if (latest?.updatedAtSnapshot && comment && comment.internal && !comment.accountId) {
        setNoteReview({ comment, version: latest.updatedAtSnapshot, audiences: latest.audiences });
      } else {
        setVisibilityError("This note is no longer available to share. Cancel and review ticket activity.");
      }
    } finally {
      setBusy(false);
    }
  };
  const openEditor = () => {
    resetOperation();
    setInternal(true);
    setExpanded(true);
  };
  const closeEditor = () => {
    setExpanded(false);
    setText("");
  };
  const submit = async () => {
    if (!text.trim() || blocked) return;
    setBusy(true);
    try {
      if (await onAdd(text, operationKey({ ticketId, text, internal }), { internal })) closeEditor();
    } finally {
      setBusy(false);
    }
  };
  const changeVisibility = async (id: string, nextInternal: boolean, version: Timestamp) => {
    if (!onVisibilityChange || blocked) return;
    setBusy(true);
    setVisibilityError(null);
    try {
      if (await onVisibilityChange(id, nextInternal, version, setVisibilityError)) setSharingNote(null);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col gap-3">
      <h3 className="text-emphasis-300 font-medium">Activity</h3>
      <div className="flex flex-col divide-y divide-border-5">
        {comments.map((comment) => (
          <div key={comment.id} className="flex py-3 first:pt-0 last:pb-0">
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="flex items-center gap-2">
                <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 gap-y-1">
                  <span className="text-emphasis-200 font-medium">{comment.userName}</span>
                  <span className="text-200 text-text-primary-70">{comment.createdAt?.toLocaleString() ?? ""}</span>
                </div>
                {canManage && comment.authoredByCaller ? (
                  <Button
                    className="ml-auto min-h-9 min-w-9"
                    prefixIcon={<Trash width={iconSizes.small} />}
                    textColor="text-text-primary"
                    variant={variants.textOnly}
                    textOnlyUnderlineOnHover={false}
                    size={buttonSizes.compact}
                    ariaLabel="Delete comment"
                    disabled={blocked}
                    onClick={() => void onDelete(comment.id)}
                  />
                ) : null}
              </div>
              <span className="text-300 wrap-anywhere whitespace-pre-wrap">{comment.text}</span>
              <div className="mt-1 flex items-center">
                {canManage && !comment.accountId && onVisibilityChange ? (
                  <Switch
                    label="Internal only"
                    checked={comment.internal}
                    disabled={blocked || !ticketVersion || (comment.internal && !sharedWithClients)}
                    setChecked={() => {
                      if (!ticketVersion) return;
                      if (comment.internal) {
                        setVisibilityError(null);
                        setNoteReview(null);
                        setSharingNote({ comment, version: ticketVersion, audiences });
                      } else void changeVisibility(comment.id, true, ticketVersion);
                    }}
                  />
                ) : (
                  <span className="text-200 text-text-primary-50">
                    {comment.internal
                      ? "Internal only"
                      : comment.accountId
                        ? `Visible to ${comment.accountName}`
                        : "Visible to all affected clients"}
                  </span>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>
      {canManage && !expanded ? (
        <Button
          className="self-start"
          text="Add comment"
          variant={variants.secondary}
          size={buttonSizes.compact}
          onClick={openEditor}
          disabled={blocked}
        />
      ) : null}
      {canManage && expanded ? (
        <div className="flex flex-col gap-3">
          <Textarea
            id={`comment-${ticketId}`}
            label="Add a comment"
            initValue={text}
            disabled={blocked}
            maxLength={4096}
            onChange={setText}
            rows={3}
          />
          {!internal ? (
            <p className="text-200 text-text-primary-70">
              Visible to: {audiences.map((a) => a.name).join(", ") || "All affected clients"}
            </p>
          ) : null}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <Switch
              label="Internal only"
              checked={internal}
              disabled={blocked || !sharedWithClients}
              setChecked={setInternal}
            />
            <div className="ml-auto flex gap-2">
              <Button
                text="Cancel"
                variant={variants.ghost}
                size={buttonSizes.compact}
                onClick={closeEditor}
                disabled={blocked}
              />
              <Button
                text="Post"
                variant={variants.primary}
                size={buttonSizes.compact}
                onClick={() => void submit()}
                disabled={blocked || !text.trim()}
                loading={busy}
              />
            </div>
          </div>
        </div>
      ) : null}
      {!sharingNote && (visibilityError || error) ? <div role="alert">{visibilityError || error}</div> : null}
      {sharingNote ? (
        <Dialog
          open
          manageFocus
          title="Share note with clients?"
          onDismiss={blocked ? undefined : cancelSharing}
          buttons={[
            { text: "Cancel", variant: variants.secondary, disabled: blocked, onClick: cancelSharing },
            {
              text: "Share note",
              variant: variants.primary,
              loading: busy,
              disabled: disabled || noteReview !== null,
              onClick: () => void changeVisibility(sharingNote.comment.id, false, sharingNote.version),
            },
          ]}
        >
          <div className="flex flex-col gap-3">
            <p>Visible to: {sharingNote.audiences.map((a) => a.name).join(", ") || "All affected clients"}</p>
            <p className="whitespace-pre-wrap">{sharingNote.comment.text}</p>
            {visibilityError ? <p role="alert">{visibilityError}</p> : null}
            {visibilityError && onRefresh && !noteReview ? (
              <Button
                text="Refresh note for review"
                variant={variants.secondary}
                disabled={blocked}
                onClick={() => void refreshNote()}
              />
            ) : null}
            {noteReview ? (
              <div className="flex flex-col gap-3">
                <p>Review the refreshed note and recipients before sharing again.</p>
                <p>Visible to: {noteReview.audiences.map((a) => a.name).join(", ") || "All affected clients"}</p>
                <p className="whitespace-pre-wrap">{noteReview.comment.text}</p>
                <Button
                  text="Use refreshed note"
                  variant={variants.secondary}
                  disabled={blocked}
                  onClick={() => {
                    setSharingNote(noteReview);
                    setNoteReview(null);
                    setVisibilityError(null);
                  }}
                />
              </div>
            ) : null}
          </div>
        </Dialog>
      ) : null}
    </div>
  );
};
export default TicketComments;
