import { useId } from "react";

import ChangePreviewTable from "./ChangePreviewTable";
import type { getChannelSettingsChanges, MinerScopeChanges } from "./channelSettingsChangesUtils";
import Button, { variants } from "@/shared/components/Button";

interface ChannelSettingsChangesProps {
  changes: ReturnType<typeof getChannelSettingsChanges>;
  onViewMiners: (changes: MinerScopeChanges, trigger: HTMLButtonElement) => void;
}

export default function ChannelSettingsChanges({
  changes: { changes, scopeChanges, minerChanges },
  onViewMiners,
}: ChannelSettingsChangesProps) {
  const headingId = useId();
  if (changes.length === 0 && scopeChanges.length === 0) return null;

  return (
    <section aria-labelledby={headingId} className="grid gap-2">
      <h3 id={headingId} className="text-emphasis-300 text-text-primary">
        Channel settings changes
      </h3>
      <ChangePreviewTable
        label="Channel settings changes"
        subject="Setting"
        rows={[
          ...changes.map(({ label, before: original, after: target }) => ({ key: label, label, original, target })),
          ...scopeChanges.map(({ label, before: original, after: target }) => ({
            key: `scope-${label}`,
            label: `Applies to · ${label}`,
            original,
            target,
          })),
        ]}
      />
      {minerChanges ? (
        <div className="flex flex-wrap items-center justify-between gap-2 text-200">
          <span className="text-text-primary-70">
            {minerChanges.addedCount.toLocaleString()} added · {minerChanges.removedCount.toLocaleString()} removed
          </span>
          <Button
            variant={variants.textOnly}
            text="View miners"
            ariaHasPopup="dialog"
            onClick={(event) => onViewMiners(minerChanges, event.currentTarget)}
          />
        </div>
      ) : null}
    </section>
  );
}
