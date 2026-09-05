import { useState } from "react";
import type { TicketCommentItem } from "../../types";
import { DismissTiny } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Textarea from "@/shared/components/Textarea";

interface TicketCommentsProps {
  ticketId: string;
  comments: TicketCommentItem[];
  canManage: boolean;
  error?: string | null;
  onAdd: (text: string) => Promise<boolean>;
  onDelete: (id: string) => Promise<boolean>;
}
const TicketComments = ({ ticketId, comments, canManage, error, onAdd, onDelete }: TicketCommentsProps) => {
  const [text, setText] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!text.trim()) return;
    setBusy(true);
    const ok = await onAdd(text);
    setBusy(false);
    if (ok) {
      setText("");
      setExpanded(false);
    }
  };
  return (
    <div className="flex flex-col gap-3">
      <span className="text-emphasis-300 font-medium">Activity</span>
      {canManage && expanded ? (
        <div className="flex flex-col gap-2">
          <Textarea id={`comment-${ticketId}`} label="Add a comment" onChange={setText} rows={3} />
          <div className="flex justify-end gap-2">
            <Button
              text="Cancel"
              variant={variants.ghost}
              size={buttonSizes.compact}
              onClick={() => setExpanded(false)}
            />
            <Button
              text="Post"
              variant={variants.primary}
              size={buttonSizes.compact}
              onClick={() => void submit()}
              disabled={!text.trim()}
              loading={busy}
            />
          </div>
        </div>
      ) : canManage ? (
        <button type="button" className="text-left text-300 underline" onClick={() => setExpanded(true)}>
          Add comment
        </button>
      ) : null}
      {error ? <div role="alert">{error}</div> : null}
      <div className="flex flex-col">
        {comments.map((comment) => (
          <div key={comment.id} className="group flex gap-3 pb-4">
            <div className="mt-1.5 h-2 w-2 rounded-full bg-intent-success-fill" />
            <div className="flex flex-1 flex-col">
              <div className="flex items-center gap-2">
                <span className="text-emphasis-200 font-medium">{comment.userName}</span>
                <span className="text-200 text-text-primary-70">{comment.createdAt?.toLocaleString() ?? ""}</span>
                {canManage && comment.authoredByCaller ? (
                  <Button
                    className="ml-auto"
                    prefixIcon={<DismissTiny />}
                    variant={variants.ghost}
                    size={buttonSizes.compact}
                    ariaLabel="Delete comment"
                    onClick={() => void onDelete(comment.id)}
                  />
                ) : null}
              </div>
              <span className="text-300">{comment.text}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};
export default TicketComments;
