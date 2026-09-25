import { useId } from "react";

import ChangePreviewTable from "./ChangePreviewTable";
import type { getChannelSettingsChanges } from "./channelSettingsChangesUtils";

interface ChannelSettingsChangesProps {
  changes: ReturnType<typeof getChannelSettingsChanges>;
}

export default function ChannelSettingsChanges({ changes: { changes, scopeChanges } }: ChannelSettingsChangesProps) {
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
    </section>
  );
}
