import type { ReactElement, ReactNode } from "react";

import type {
  ReleaseChannelDraft,
  ReleaseChannelFile,
  ReleaseChannelPreview,
  ReleaseChannelScope,
} from "./releaseChannelTypes";
import RolloutControls from "./RolloutControls";
import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import TargetSelectButton, { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";
import { Ellipsis } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Textarea from "@/shared/components/Textarea";

interface ReleaseChannelModalProps {
  open: boolean;
  /** Create shows "Create release channel"; manage shows "Manage release channel". */
  mode: "create" | "manage";
  draft: ReleaseChannelDraft;
  onDraftChange: (next: ReleaseChannelDraft) => void;
  preview: ReleaseChannelPreview;
  onAddFile: () => void;
  onFileActions: (file: ReleaseChannelFile) => void;
  /** Per-scope-level selection entry points (Sites / Buildings / etc.). */
  onSelectScope: (level: keyof ReleaseChannelScope) => void;
  onDismiss: () => void;
  onSave: () => void;
}

function SectionTitle({ children }: { children: string }): ReactElement {
  return <div className="text-emphasis-300 text-text-primary">{children}</div>;
}

function Section({
  title,
  action,
  children,
}: {
  title: string;
  action?: ReactNode;
  children: ReactNode;
}): ReactElement {
  return (
    <section className="grid gap-3">
      <div className="flex items-center justify-between gap-4">
        <SectionTitle>{title}</SectionTitle>
        {action}
      </div>
      {children}
    </section>
  );
}

// ---- Firmware file table (inside the modal) --------------------------------

type FirmwareFileColumn = "model" | "file" | "uploaded" | "actions";

const firmwareFileColumns: FirmwareFileColumn[] = ["model", "file", "uploaded", "actions"];

const firmwareFileColTitles: ColTitles<FirmwareFileColumn> = {
  model: "Model",
  file: "File",
  uploaded: "Uploaded",
  actions: "",
};

// ---- Scope rows (Apply to) -------------------------------------------------

const scopeLevels: Array<{ level: keyof ReleaseChannelScope; label: string; singular: string }> = [
  { level: "sites", label: "Sites", singular: "site" },
  { level: "buildings", label: "Buildings", singular: "building" },
  { level: "racks", label: "Racks", singular: "rack" },
  { level: "groups", label: "Groups", singular: "group" },
  { level: "miners", label: "Miners", singular: "miner" },
];

// ---- Coverage preview pane -------------------------------------------------

function CoveragePreview({ preview }: { preview: ReleaseChannelPreview }): ReactElement {
  const scopeSummary = [
    `${preview.siteCount} ${preview.siteCount === 1 ? "site" : "sites"}`,
    `${preview.buildingCount} ${preview.buildingCount === 1 ? "building" : "buildings"}`,
    `${preview.rackCount} ${preview.rackCount === 1 ? "rack" : "racks"}`,
  ];

  return (
    <div className="flex min-h-[360px] flex-1 flex-col gap-10 rounded-[24px] bg-surface-overlay px-8 py-12 laptop:min-h-0 laptop:px-16 laptop:py-10">
      <div className="flex flex-col gap-8">
        <div className="text-heading-100 text-text-primary">
          Deploys firmware to {preview.minerCount.toLocaleString()} miners ({preview.modelCount}{" "}
          {preview.modelCount === 1 ? "model" : "models"}) across {scopeSummary.join(", ")}.
        </div>

        <div className="grid gap-3">
          <div className="text-emphasis-300 text-text-primary">Previous updates</div>
          <div className="grid divide-y divide-border-5">
            {preview.previousRollouts.map((rollout) => (
              <div key={rollout} className="py-3 text-300 text-text-primary-70">
                {rollout}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

/** Create/manage surface for a firmware release channel. */
function ReleaseChannelModal({
  open,
  mode,
  draft,
  onDraftChange,
  preview,
  onAddFile,
  onFileActions,
  onSelectScope,
  onDismiss,
  onSave,
}: ReleaseChannelModalProps): ReactElement {
  const title = mode === "create" ? "Create release channel" : "Manage release channel";
  const closeAriaLabel = mode === "create" ? "Close release channel creator" : "Close release channel editor";

  const firmwareFileColConfig: ColConfig<ReleaseChannelFile, string, FirmwareFileColumn> = {
    model: {
      component: (file) => <span className="text-emphasis-300 text-text-primary">{file.model}</span>,
      width: "w-40",
    },
    file: { component: (file) => file.file, width: "w-64" },
    uploaded: { component: (file) => file.uploaded, width: "w-48" },
    actions: {
      component: (file) => (
        <div className="flex justify-end">
          <button
            type="button"
            aria-label={`${file.file} actions`}
            className="flex h-8 w-8 items-center justify-center text-text-primary hover:opacity-70"
            onClick={() => onFileActions(file)}
          >
            <Ellipsis />
          </button>
        </div>
      ),
      width: "w-16",
    },
  };

  const previewPane = <CoveragePreview preview={preview} />;

  const buttons: NonNullable<FullScreenTwoPaneModalProps["buttons"]> = [
    {
      text: "Save",
      variant: variants.primary,
      onClick: onSave,
    },
  ];

  return (
    <FullScreenTwoPaneModal
      open={open}
      title={title}
      closeAriaLabel={closeAriaLabel}
      onDismiss={onDismiss}
      buttons={buttons}
      abovePanes={<div className="px-6 pb-6 laptop:hidden">{previewPane}</div>}
      primaryPane={
        <section className="flex flex-col gap-12 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
          <Section title="General">
            <div className="grid gap-3">
              <Input
                id="release-channel-name"
                label="Name"
                type="text"
                initValue={draft.name}
                onChange={(value) => onDraftChange({ ...draft, name: value })}
              />
              <Textarea
                id="release-channel-description"
                label="Description"
                rows={3}
                initValue={draft.description}
                onChange={(value) => onDraftChange({ ...draft, description: value })}
              />
            </div>
          </Section>

          <Section
            title="Firmware"
            action={<Button text="Add file" variant={variants.secondary} size={sizes.compact} onClick={onAddFile} />}
          >
            <div className="text-300 text-text-primary-70">
              Add one file per hardware model. A channel can span multiple models. Subscribed miners receive the file
              that matches their model.
            </div>
            <List<ReleaseChannelFile, string, FirmwareFileColumn>
              activeCols={firmwareFileColumns}
              colTitles={firmwareFileColTitles}
              colConfig={firmwareFileColConfig}
              items={draft.files}
              itemKey="id"
              total={draft.files.length}
              itemName={{ singular: "firmware file", plural: "firmware files" }}
              hideTotal
              applyColumnWidthsToCells
              stickyFirstColumn={false}
            />
          </Section>

          <Section title="Applies to">
            <div className="grid">
              {scopeLevels.map(({ level, label, singular }) => (
                <TargetSelectButton
                  key={level}
                  label={label}
                  value={getTargetButtonLabel(draft.scope[level], singular)}
                  size={sizes.compact}
                  onClick={() => onSelectScope(level)}
                />
              ))}
            </div>
          </Section>

          <RolloutControls config={draft.config} onChange={(config) => onDraftChange({ ...draft, config })} />
        </section>
      }
      secondaryPane={previewPane}
      secondaryPaneClassName="!hidden !bg-transparent laptop:!flex laptop:!pl-0 laptop:!rounded-[24px]"
    />
  );
}

export default ReleaseChannelModal;
