import { useId } from "react";

import ChangePreviewTable from "./ChangePreviewTable";
import { getChannelSettingsChanges } from "./channelSettingsChangesUtils";
import type { ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";

interface ChannelSettingsChangesProps {
  before: ReleaseChannelDraft;
  after: ReleaseChannelDraft;
  minerNames: Record<string, string>;
}

export default function ChannelSettingsChanges({ before, after, minerNames }: ChannelSettingsChangesProps) {
  const headingId = useId();
  const { changes, scopeChanges } = getChannelSettingsChanges(before, after, minerNames);
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
