import type { ReactNode } from "react";
import Button from "@/shared/components/Button";

export function ConfigurationUpdateRow({
  time,
  summary,
  onView,
}: {
  time?: Date;
  summary: ReactNode;
  onView: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div>
        <p>{time?.toLocaleString() ?? "Pool update"}</p>
        <p className="text-200 text-text-primary-50">{summary}</p>
      </div>
      <Button variant="textOnly" text="View results" onClick={onView} />
    </div>
  );
}
