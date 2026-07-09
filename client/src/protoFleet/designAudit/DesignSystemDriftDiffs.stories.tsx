import { ReactNode } from "react";

import { ChevronDown, Copy, Edit, Ellipsis, Grip, Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Row from "@/shared/components/Row";
import Select from "@/shared/components/Select";

const specimenClassName =
  "grid gap-4 rounded-2xl border border-border-5 bg-surface-elevated-base p-5 shadow-50 laptop:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_280px]";

const specimenHeaderClassName = "mb-3 text-200 text-text-primary-50";
const specimenPanelClassName = "min-w-0 rounded-xl border border-border-5 bg-surface-base p-4";
const specimenTitleClassName = "mb-2 text-emphasis-300 text-text-primary";
const specimenNoteClassName = "text-200 text-text-primary-70";
const specimenMetadataLabelClassName = "mb-1 text-emphasis-200 text-text-primary-50";
const migrationChipClassName =
  "inline-flex max-w-full items-center rounded-full bg-core-primary-5 px-2.5 py-1 font-mono text-200 text-text-primary";
const locationChipClassName =
  "rounded-md bg-surface-overlay px-2 py-1 font-mono text-100 leading-4 text-text-primary-70";

type Risk = "low" | "medium" | "high";

const riskTone: Record<Risk, string> = {
  low: "bg-intent-success-10 text-intent-success-text",
  medium: "bg-intent-warning-10 text-intent-warning-text",
  high: "bg-intent-critical-10 text-intent-critical-text",
};

const Shell = ({ children }: { children: ReactNode }) => (
  <div className="min-h-screen bg-surface-5 p-8 text-text-primary">
    <div className="mx-auto flex max-w-[1280px] flex-col gap-6">
      <div>
        <div className="text-heading-300 text-text-primary">Design-system drift visual diffs</div>
        <div className="mt-2 max-w-[760px] text-300 text-text-primary-70">
          These are prioritized specimens, not a closed inventory. Current specimens recreate representative bespoke or
          drifted implementations; proposed specimens use Storybook-backed primitives and tokens already defined in the
          shared design system.
        </div>
      </div>
      {children}
    </div>
  </div>
);

const Specimen = ({
  title,
  risk,
  migrations,
  before,
  after,
  notes,
  locations,
}: {
  title: string;
  risk: Risk;
  migrations: string[];
  before: ReactNode;
  after: ReactNode;
  notes: ReactNode;
  locations?: string[];
}) => (
  <section className={specimenClassName} data-testid={`specimen-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`}>
    <div className="min-w-0">
      <div className={specimenHeaderClassName}>Current</div>
      <div className={specimenPanelClassName}>{before}</div>
    </div>
    <div className="min-w-0">
      <div className={specimenHeaderClassName}>Proposed</div>
      <div className={specimenPanelClassName}>{after}</div>
    </div>
    <div className="min-w-0">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className={specimenTitleClassName}>{title}</div>
        <span className={`shrink-0 rounded-full px-2 py-0.5 text-200 ${riskTone[risk]}`}>{risk}</span>
      </div>
      <div className="mb-3">
        <div className={specimenMetadataLabelClassName}>Migration</div>
        <div className="flex flex-col gap-2">
          {migrations.map((migration) => (
            <span key={migration} className={migrationChipClassName}>
              {migration}
            </span>
          ))}
        </div>
      </div>
      {locations !== undefined && locations.length > 0 ? (
        <div className="mb-3">
          <div className={specimenMetadataLabelClassName}>Occurs in</div>
          <div className="flex flex-col gap-1.5">
            {locations.map((location) => (
              <span key={location} className={locationChipClassName}>
                {location}
              </span>
            ))}
          </div>
        </div>
      ) : null}
      <div className={specimenNoteClassName}>{notes}</div>
    </div>
  </section>
);

const CurrentFileUpload = () => (
  <div className="flex flex-col gap-3">
    <div className="ring-border-focus flex cursor-pointer flex-col items-center justify-center gap-4 rounded-2xl bg-grayscale-gray-5 p-10 ring-2 transition-colors">
      <div className="text-300 text-text-primary">Drag update files here</div>
      <div className="text-text-secondary text-200">or</div>
      <button
        type="button"
        className="hover:bg-surface-secondary rounded-full border border-border-20 bg-surface-elevated-base px-5 py-2 text-300 text-text-primary transition-colors"
      >
        Choose file
      </button>
    </div>
    <div className="text-text-secondary text-200">Supported file types: .tar, .tar.gz</div>
  </div>
);

const ProposedFileUpload = () => (
  <div className="flex flex-col gap-3">
    <div className="flex cursor-pointer flex-col items-center justify-center gap-4 rounded-2xl bg-grayscale-gray-5 p-10 ring-2 ring-border-primary transition-colors">
      <div className="text-300 text-text-primary">Drag update files here</div>
      <div className="text-200 text-text-primary-70">or</div>
      <Button text="Choose file" variant={variants.secondary} size={sizes.base} />
    </div>
    <div className="text-200 text-text-primary-70">Supported file types: .tar, .tar.gz</div>
  </div>
);

const CurrentSitePicker = () => (
  <div className="flex w-full max-w-[320px] flex-col gap-3">
    <button
      type="button"
      className="hover:bg-surface-base-hover flex max-w-full min-w-0 items-center gap-1 rounded-md px-2 py-1 text-300 text-text-primary focus-visible:underline"
    >
      <span className="min-w-0 truncate">Denver, CO</span>
      <ChevronDown className={iconSizes.xSmall} />
    </button>
    <div className="rounded-xl border border-border-5 p-1">
      <button className="hover:bg-surface-base-hover focus-visible:bg-surface-base-hover flex w-full items-center gap-3 rounded-md px-2 py-2.5 text-left text-300 text-text-primary">
        <span className="h-5 w-5 rounded-full border border-border-20" />
        All sites
      </button>
      <button className="hover:bg-surface-base-hover focus-visible:bg-surface-base-hover flex w-full items-center gap-3 rounded-md px-2 py-2.5 text-left text-300 text-text-primary">
        <span className="grid h-5 w-5 place-items-center rounded-full bg-core-accent-fill text-text-contrast">
          <span className="h-2 w-2 rounded-full bg-text-contrast" />
        </span>
        Denver, CO
      </button>
    </div>
  </div>
);

const ProposedSitePicker = () => (
  <div className="flex w-full max-w-[320px] flex-col gap-3">
    <button
      type="button"
      className="inline-flex max-w-full min-w-0 items-center gap-1 self-start rounded-md px-2 py-1 text-300 text-text-primary hover:bg-core-primary-5 focus-visible:bg-core-primary-5"
    >
      <span className="min-w-0 truncate">Denver, CO</span>
      <ChevronDown className={iconSizes.xSmall} />
    </button>
    <div className="rounded-xl border border-border-5 p-1">
      <button className="flex w-full items-center gap-3 rounded-md px-2 py-2.5 text-left text-300 text-text-primary hover:bg-core-primary-5 focus-visible:bg-core-primary-5">
        <span className="h-5 w-5 rounded-full border border-border-20" />
        All sites
      </button>
      <button className="flex w-full items-center gap-3 rounded-md px-2 py-2.5 text-left text-300 text-text-primary hover:bg-core-primary-5 focus-visible:bg-core-primary-5">
        <span className="grid h-5 w-5 place-items-center rounded-full bg-core-accent-fill text-text-contrast">
          <span className="h-2 w-2 rounded-full bg-text-contrast" />
        </span>
        Denver, CO
      </button>
    </div>
  </div>
);

const CurrentCopySecret = () => (
  <div className="flex items-center justify-between gap-2 rounded-xl bg-core-primary-5 px-6 py-6">
    <div className="font-mono text-300 break-all text-text-primary">pf_live_1234_abcd_5678_efgh</div>
    <button className="shrink-0 text-text-primary" aria-label="Copy API key">
      <Copy />
    </button>
  </div>
);

const ProposedCopySecret = () => (
  <div className="flex items-center justify-between gap-2 rounded-xl bg-core-primary-5 px-6 py-6">
    <div className="font-mono text-300 break-all text-text-primary">pf_live_1234_abcd_5678_efgh</div>
    <Button
      ariaLabel="Copy API key"
      variant={variants.textOnly}
      size={sizes.textOnly}
      prefixIcon={<Copy />}
      className="-my-2 shrink-0 !p-2 text-text-primary"
    />
  </div>
);

const CurrentInlineEdit = () => (
  <div className="grid max-w-[420px] gap-3">
    <div className="flex items-center gap-2 rounded-lg border border-border-5 px-3 py-3">
      <span className="min-w-24 text-300 text-text-primary-50">Channel</span>
      <input
        type="text"
        value="#critical-alerts"
        readOnly
        aria-label="Slack channel"
        className="bg-surface-1 w-full rounded-md border border-border-5 px-2 py-1 text-300 text-text-primary outline-none focus:border-core-primary-fill"
      />
      <button type="button" className="text-text-primary-50 hover:text-text-primary" aria-label="Edit channel">
        <Edit />
      </button>
    </div>
  </div>
);

const ProposedInlineEdit = () => (
  <div className="grid max-w-[420px] gap-3">
    <div className="flex items-center gap-3 rounded-lg border border-border-5 px-3 py-3">
      <div className="min-w-0 flex-1">
        <Input id="visual-diff-channel" label="Slack channel" initValue="#critical-alerts" />
      </div>
      <Button
        ariaLabel="Edit channel"
        variant={variants.textOnly}
        size={sizes.textOnly}
        prefixIcon={<Edit />}
        className="-my-2 !p-2 text-text-primary-70"
      />
    </div>
  </div>
);

const CurrentPicker = () => (
  <div className="max-w-[360px]">
    <button
      type="button"
      className="peer relative flex h-14 w-full items-center justify-between rounded-lg border border-border-20 bg-surface-base pr-4 pl-4 text-left ring-4 ring-core-primary-5 outline-hidden"
    >
      <div className="flex min-w-0 flex-col pt-[18px]">
        <span className="absolute top-[7px] text-200 text-text-primary-50">Target</span>
        <span className="truncate text-300 text-text-primary">Building A</span>
      </div>
      <ChevronDown width="w-3" className="shrink-0 rotate-180 text-text-primary-70" />
    </button>
    <div className="mt-2 rounded-xl border border-border-5 bg-surface-elevated-base p-1.5 shadow-300">
      {["Site", "Building A", "Building B"].map((option) => (
        <div
          key={option}
          className="flex cursor-pointer items-center gap-3 rounded-xl p-3 text-left text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
        >
          <span className="h-5 w-5 rounded-full border border-border-20" />
          <div className="truncate text-emphasis-300">{option}</div>
        </div>
      ))}
    </div>
  </div>
);

const ProposedPicker = () => (
  <div className="max-w-[360px]">
    <Select
      id="visual-diff-target"
      label="Target"
      value="building-a"
      onChange={() => undefined}
      options={[
        { value: "site", label: "Site" },
        { value: "building-a", label: "Building A" },
        { value: "building-b", label: "Building B" },
      ]}
      forceBelow
    />
  </div>
);

const CurrentMenu = () => (
  <div className="min-h-48">
    <div className="flex justify-end">
      <div className="relative inline-flex">
        <button
          type="button"
          aria-label="Building options"
          className="flex h-8 w-8 items-center justify-center rounded-lg text-text-primary-70 hover:cursor-pointer"
        >
          <Ellipsis width="w-4" />
        </button>
        <div className="absolute top-full right-0 z-30 mt-1 w-44 rounded-xl border border-border-5 bg-surface-elevated-base py-1 shadow-300">
          <button type="button" className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-2">
            View details
          </button>
          <button type="button" className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-5">
            View racks
          </button>
          <button type="button" className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-5">
            View miners
          </button>
        </div>
      </div>
    </div>
  </div>
);

const ProposedMenu = () => (
  <div className="min-h-48">
    <div className="flex justify-end">
      <div className="relative inline-flex">
        <Button
          ariaLabel="Building options"
          variant={variants.textOnly}
          size={sizes.textOnly}
          prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
          textOnlyUnderlineOnHover={false}
          className="!h-8 !w-8 !p-0"
        />
        <div className="absolute top-full right-0 z-30 mt-1 w-44 rounded-2xl border border-border-5 bg-surface-elevated-base px-0 pt-2 pb-1 shadow-300">
          {["View details", "View racks", "View miners"].map((label) => (
            <div className="px-4" key={label}>
              <Row compact divider={false} className="text-emphasis-300">
                {label}
              </Row>
            </div>
          ))}
        </div>
      </div>
    </div>
  </div>
);

const CurrentTypography = () => (
  <div className="grid gap-4">
    <div className="text-200 font-semibold tracking-[0.08em] text-text-primary-50 uppercase">When</div>
    <div className="space-y-0 text-[14px] leading-[18px] text-text-primary-50">
      <p className="truncate">Curtail all miners assigned to selected racks</p>
      <p className="truncate">Denver, CO, Building A</p>
    </div>
    <div className="flex h-9 w-9 items-center justify-center rounded-full bg-core-primary-fill text-base font-semibold text-text-contrast">
      JM
    </div>
  </div>
);

const ProposedTypography = () => (
  <div className="grid gap-4">
    <div className="text-emphasis-200 text-text-primary-50 uppercase">When</div>
    <div className="space-y-0 text-300 text-text-primary-50">
      <p className="truncate">Curtail all miners assigned to selected racks</p>
      <p className="truncate">Denver, CO, Building A</p>
    </div>
    <div className="flex h-9 w-9 items-center justify-center rounded-full bg-core-primary-fill text-emphasis-300 text-text-contrast">
      JM
    </div>
  </div>
);

const CurrentDomainControls = () => (
  <div className="flex items-center gap-4">
    <button
      type="button"
      className="cursor-grab touch-none text-text-primary hover:text-text-primary active:cursor-grabbing"
      aria-label="Reorder Hashrate"
    >
      <Grip width="w-4" className="h-4 shrink-0" />
    </button>
    <button
      type="button"
      className="flex h-12 w-12 items-center justify-center rounded-lg border border-border-10 bg-transparent text-[14px] font-medium text-text-primary-70 tabular-nums"
    >
      01
    </button>
    <button
      type="button"
      className="flex h-12 w-12 items-center justify-center rounded-full bg-core-primary-fill/6 text-core-primary-fill/25 transition-colors hover:bg-core-primary-fill/10 hover:text-core-primary-fill/50"
    >
      +
    </button>
  </div>
);

const ProposedDomainControls = () => (
  <div className="flex items-center gap-4">
    <button
      type="button"
      className="cursor-grab touch-none text-text-primary-70 hover:text-text-primary active:cursor-grabbing"
      aria-label="Reorder Hashrate"
    >
      <Grip width="w-4" className="h-4 shrink-0" />
    </button>
    <button
      type="button"
      className="flex h-12 w-12 items-center justify-center rounded-lg border border-border-10 bg-transparent text-emphasis-300 text-text-primary-70 tabular-nums"
    >
      01
    </button>
    <Button
      ariaLabel="Assign miner"
      variant={variants.secondary}
      size={sizes.compact}
      className="h-9 w-9 !rounded-full !p-0"
    >
      +
    </Button>
  </div>
);

const CurrentTooltip = () => (
  <div className="relative min-h-36">
    <button
      type="button"
      aria-label="Hashrate reporting"
      className="inline-flex h-6 w-6 items-center justify-center rounded-full border border-border-10 bg-transparent text-text-primary-50"
    >
      <Info width={iconSizes.xSmall} />
    </button>
    <div className="absolute top-8 left-0 z-50 w-80 rounded-lg bg-surface-base p-4 shadow-200">
      <div className="text-300 text-text-primary-50">Partial reporting</div>
      <div className="text-300 text-text-primary">Some miners are missing this metric.</div>
    </div>
  </div>
);

const ProposedTooltip = () => (
  <div className="relative min-h-36">
    <span className="inline-flex h-6 w-6 items-center justify-center text-text-primary-50">
      <Info width={iconSizes.small} />
    </span>
    <div className="absolute top-8 left-0 z-50 w-80 rounded-lg bg-surface-base p-4 text-text-primary shadow-200">
      <div className="mb-1 text-heading-100 text-text-primary">Partial reporting</div>
      <div className="text-300 text-text-primary-70">Some miners are missing this metric.</div>
    </div>
  </div>
);

const CurrentTypeahead = () => (
  <div className="relative max-w-[360px]">
    <Input id="visual-diff-zone-current" label="Zone (optional)" initValue="North" />
    <div className="absolute top-full z-10 mt-1 w-full rounded-xl border border-border-5 bg-surface-elevated-base p-1.5 shadow-300">
      {["North Aisle", "North Immersion", "North Wing"].map((suggestion, index) => (
        <button
          key={suggestion}
          type="button"
          className={`w-full rounded-xl px-3 py-2.5 text-left text-300 text-text-primary ${
            index === 0 ? "bg-core-primary-5" : "hover:bg-core-primary-5"
          }`}
        >
          {suggestion}
        </button>
      ))}
    </div>
  </div>
);

const ProposedTypeahead = () => (
  <div className="relative max-w-[360px]">
    <Input id="visual-diff-zone-proposed" label="Zone (optional)" initValue="North" />
    <div className="absolute top-full z-10 mt-1 w-full rounded-2xl border border-border-5 bg-surface-elevated-base px-0 pt-2 pb-1 shadow-300">
      {["North Aisle", "North Immersion", "North Wing"].map((suggestion) => (
        <div className="px-4" key={suggestion}>
          <Row compact divider={false} className="text-emphasis-300">
            {suggestion}
          </Row>
        </div>
      ))}
    </div>
  </div>
);

const CurrentCameraAction = () => (
  <div className="max-w-[360px]">
    <button
      type="button"
      className="w-full rounded-xl border border-border-10 bg-surface-5 py-4 text-300 font-medium text-text-primary hover:bg-surface-10"
    >
      Open camera
    </button>
  </div>
);

const ProposedCameraAction = () => (
  <div className="max-w-[360px]">
    <Button text="Open camera" variant={variants.secondary} size={sizes.base} className="w-full" />
  </div>
);

const CurrentLinkLikeButton = () => (
  <div className="flex items-center gap-4 text-300">
    <button type="button" className="flex cursor-pointer items-center gap-2 hover:underline">
      <span className="h-2 w-2 rounded-full bg-intent-critical-fill" />
      Needs attention
    </button>
    <button type="button" className="cursor-pointer transition-opacity hover:opacity-80" aria-label="View issues">
      <span className="font-mono text-200 text-intent-critical-fill">!</span>
    </button>
  </div>
);

const ProposedLinkLikeButton = () => (
  <div className="flex items-center gap-4 text-300">
    <Button
      variant={variants.textOnly}
      size={sizes.textOnly}
      text="Needs attention"
      prefixIcon={<span className="h-2 w-2 rounded-full bg-intent-critical-fill" />}
      className="!p-0 text-text-primary"
    />
    <Button
      ariaLabel="View issues"
      variant={variants.textOnly}
      size={sizes.textOnly}
      prefixIcon={<span className="font-mono text-200 text-intent-critical-fill">!</span>}
      textOnlyUnderlineOnHover={false}
      className="!p-0"
    />
  </div>
);

export const AllSpecimens = () => (
  <Shell>
    <Specimen
      title="Token cleanup: firmware upload"
      risk="low"
      migrations={[
        "text-text-secondary -> text-text-primary-70",
        "ring-border-focus -> ring-border-primary",
        "hover:bg-surface-secondary -> hover:bg-core-primary-5",
        "bare choose-file button -> Button secondary",
      ]}
      before={<CurrentFileUpload />}
      after={<ProposedFileUpload />}
      notes="This specimen shows drag-active state. Keep the strong black active ring; only migrate the undefined token name to a defined border token. The hidden file input should remain native."
    />
    <Specimen
      title="Token cleanup: site picker"
      risk="low"
      migrations={[
        "hover:bg-surface-base-hover -> hover:bg-core-primary-5",
        "trigger flex -> inline-flex self-start",
        "keep existing row density",
      ]}
      before={<CurrentSitePicker />}
      after={<ProposedSitePicker />}
      notes="Primitive migration is rejected here: Button textOnly adds an underline affordance and Row changes inner padding. The safer fix is token-only."
    />
    <Specimen
      title="Raw icon button: copy secret"
      risk="low"
      migrations={["raw copy icon button -> Button textOnly icon button"]}
      before={<CurrentCopySecret />}
      after={<ProposedCopySecret />}
      notes="Keep the same compact footprint, but move the control onto Button textOnly so focus, disabled, hover, and spacing behavior come from the primitive."
    />
    <Specimen
      title="Bare input: inline editable cell"
      risk="medium"
      migrations={["bg-surface-1 -> bg-surface-base", "bare inline input -> Input or InlineEditableField"]}
      before={<CurrentInlineEdit />}
      after={<ProposedInlineEdit />}
      notes="Replace the hand-styled input using bg-surface-1 with Input plus a Button icon affordance, or extract a reusable inline edit primitive from this shape."
    />
    <Specimen
      title="Bespoke picker: alerts single select"
      risk="medium"
      migrations={["SinglePickerField -> shared Select"]}
      before={<CurrentPicker />}
      after={<ProposedPicker />}
      notes="SinglePickerField largely duplicates Select: floating label, popover width, radio options, and focus ring. The proposed shape uses the Storybook-backed Select primitive."
    />
    <Specimen
      title="Bespoke menu: row actions"
      risk="medium"
      migrations={["hand-built menu -> RowActionsMenu pattern"]}
      before={<CurrentMenu />}
      after={<ProposedMenu />}
      locations={[
        "sites/ManageSiteModal.tsx:104-128",
        "buildings/BuildingCard.tsx:233-348, 370-379",
        "fleetManagement/MinersPane.tsx:174-204",
      ]}
      notes="Representative action-menu specimen. The production issue is duplicated trigger, overlay, shell, and item styling; final positioning should come from Popover bottom right with offset, not from this static card."
    />
    <Specimen
      title="Typography scale drift"
      risk="low"
      migrations={[
        "text-[14px] -> text-300",
        "text-base -> text-emphasis-300",
        "font-semibold/tracking overrides -> text-emphasis-*",
      ]}
      before={<CurrentTypography />}
      after={<ProposedTypography />}
      notes="Replace text-[14px], text-base, font-semibold, and tracking overrides with text-300/text-emphasis-* tokens from Foundation/Typography."
    />
    <Specimen
      title="Keep but polish: domain controls"
      risk="medium"
      migrations={["keep domain buttons", "cell popover -> shared Popover shell", "normalize tokens/font sizing only"]}
      before={<CurrentDomainControls />}
      after={<ProposedDomainControls />}
      locations={["buildings/BuildingGridPane.tsx:62-100", "fleetManagement/RackPane.tsx:37-112"]}
      notes="Rack slots and drag handles are valid domain controls. The cell popovers should not become RowActionsMenu; migrate the shell/items carefully while keeping edge-aware anchoring."
    />
    <Specimen
      title="Bespoke tooltip shells"
      risk="medium"
      migrations={["custom floating tooltip -> shared Tooltip", "interactive hover menu -> Popover"]}
      before={<CurrentTooltip />}
      after={<ProposedTooltip />}
      locations={[
        "DeviceSetList/StatCell.tsx:7-39",
        "MinerList/UnsupportedMetric.tsx:7-24",
        "BuildingRackGrid.tsx:307-341",
        "BuildingSummaryCard.tsx:103-136",
      ]}
      notes="The proposed panel is a static preview of the shared Tooltip shell because Tooltip opens on hover. Interactive hover content, like miner group links, should use Popover instead of Tooltip."
    />
    <Specimen
      title="Bespoke typeahead dropdown"
      risk="medium"
      migrations={["local suggestion dropdown -> shared Popover/Row shell", "consider Typeahead primitive"]}
      before={<CurrentTypeahead />}
      after={<ProposedTypeahead />}
      locations={["RackSettingsModal.tsx:316-360"]}
      notes="This should stay a free-text typeahead, not a plain Select. The cleanup target is shared shell, option row, focus, and keyboard behavior."
    />
    <Specimen
      title="Raw command button: camera action"
      risk="low"
      migrations={["raw full-width camera button -> Button secondary", "hidden file input stays native"]}
      before={<CurrentCameraAction />}
      after={<ProposedCameraAction />}
      locations={["ScanMinerQrModalView.tsx:218-231"]}
      notes="The visible command should inherit shared Button focus/hover/disabled states. The hidden file input remains a valid native exception."
    />
    <Specimen
      title="Link-like table buttons"
      risk="medium"
      migrations={[
        "raw underline button -> Button textOnly or real Link",
        "raw alert icon button -> Button textOnly icon",
      ]}
      before={<CurrentLinkLikeButton />}
      after={<ProposedLinkLikeButton />}
      locations={["MinerIssues.tsx:123", "MinerStatus.tsx:22", "MinerName.tsx:56"]}
      notes="Use real links for navigation. For in-place actions, keep the link-like affordance but move focus, disabled, and hit-area behavior onto Button."
    />
  </Shell>
);

export default {
  title: "Proto Fleet/Design Audit/Visual Diffs",
  parameters: {
    layout: "fullscreen",
  },
};
