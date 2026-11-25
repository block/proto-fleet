import type { ComponentProps, ReactNode } from "react";

import { ActionModal } from "@/shared/components/ActionModal";

type DialogActions = Pick<
  ComponentProps<typeof ActionModal>,
  "busy" | "error" | "readOnly" | "valid" | "onDismiss" | "onSubmit" | "confirm" | "cancel"
>;

type ReviewGroup = {
  id: string;
  name: string;
  status?: string;
  changes: Array<{ id: string; label: string; before: string; after: string }>;
  details?: ReactNode;
};

export function WorkerNameReviewDialog({
  confirm = "Save and update equipment",
  groups,
  valid = groups.length > 0,
  summary,
  children,
  ...actions
}: DialogActions & {
  groups: ReviewGroup[];
  summary?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <ActionModal title="Review worker names" confirm={confirm} presentation="dialog" valid={valid} {...actions}>
      {summary}
      {groups.length ? (
        <div className="max-h-72 overflow-auto" data-dd-privacy="mask">
          {groups.map(({ id, name, status, changes, details }) => (
            <section key={id} className="my-4">
              <h3 className="font-medium">
                {name}
                {status ? ` — ${status}` : null}
              </h3>
              <ul>
                {changes.map(({ id, label, before, after }) => (
                  <li key={id}>
                    {label}: {before || "No worker name"} → {after || "No worker name"}
                  </li>
                ))}
              </ul>
              {details}
            </section>
          ))}
        </div>
      ) : (
        <p role="status">Worker names are unchanged.</p>
      )}
      {children}
    </ActionModal>
  );
}
