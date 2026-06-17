import { useCallback, useState } from "react";

import { DismissTiny } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Textarea from "@/shared/components/Textarea";

interface TicketCommentsProps {
  ticketId: string;
}

interface ActivityEntry {
  id: string;
  type: "comment" | "system";
  userName: string;
  text: string;
  createdAt: string;
}

const MOCK_ACTIVITY: ActivityEntry[] = [
  { id: "a1", type: "system", userName: "System", text: "Ticket created", createdAt: "2d ago" },
  { id: "a2", type: "system", userName: "System", text: "Status changed to In Progress", createdAt: "1d ago" },
  { id: "a3", type: "comment", userName: "Alex K.", text: "Checked the hashboard — confirmed dead. Need replacement part from inventory.", createdAt: "23h ago" },
  { id: "a4", type: "comment", userName: "Maria S.", text: "Replacement part allocated from Denver B2-01 bin.", createdAt: "18h ago" },
];

const TicketComments = ({ ticketId }: TicketCommentsProps) => {
  const [entries] = useState<ActivityEntry[]>(MOCK_ACTIVITY);
  const [newComment, setNewComment] = useState("");
  const [isExpanded, setIsExpanded] = useState(false);

  const handleAddComment = useCallback(() => {
    if (!newComment.trim()) return;
    setNewComment("");
    setIsExpanded(false);
  }, [newComment]);

  return (
    <div className="flex flex-col gap-3">
      <span className="text-emphasis-300 font-medium">Activity</span>

      {isExpanded ? (
        <div className="flex flex-col gap-2">
          <Textarea
            id={`comment-${ticketId}`}
            label="Add a comment"
            onChange={(value) => setNewComment(value)}
            rows={3}
          />
          <div className="flex justify-end gap-2">
            <Button
              text="Cancel"
              variant={variants.ghost}
              size={buttonSizes.compact}
              onClick={() => {
                setIsExpanded(false);
                setNewComment("");
              }}
            />
            <Button
              text="Post"
              variant={variants.primary}
              size={buttonSizes.compact}
              onClick={handleAddComment}
              disabled={!newComment.trim()}
            />
          </div>
        </div>
      ) : (
        <button
          type="button"
          className="cursor-pointer text-left text-300 text-text-primary underline decoration-border-10 underline-offset-2 hover:decoration-border-20"
          onClick={() => setIsExpanded(true)}
        >
          Add comment
        </button>
      )}

      <div className="flex flex-col">
        {entries.map((entry, i) => (
          <div key={entry.id} className="relative flex gap-3 pb-4">
            <div className="flex flex-col items-center">
              <div
                className={`mt-1.5 h-2 w-2 flex-shrink-0 rounded-full ${
                  entry.type === "comment" ? "bg-intent-success-fill" : "bg-border-20"
                }`}
              />
              {i < entries.length - 1 && (
                <div className="w-px flex-1 bg-border-5" />
              )}
            </div>
            <div className="flex flex-1 flex-col gap-0.5 pb-1">
              <div className="flex items-center gap-2">
                <span className="text-emphasis-200 font-medium">{entry.userName}</span>
                <span className="text-200 text-text-primary-70">{entry.createdAt}</span>
                {entry.type === "comment" && (
                  <Button
                    className="ml-auto opacity-0 group-hover:opacity-100"
                    prefixIcon={<DismissTiny />}
                    variant={variants.ghost}
                    size={buttonSizes.compact}
                    ariaLabel="Delete comment"
                  />
                )}
              </div>
              <span className="text-300">{entry.text}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

export default TicketComments;
