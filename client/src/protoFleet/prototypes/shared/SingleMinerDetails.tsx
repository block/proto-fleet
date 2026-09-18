/**
 * The informational chrome for a connected miner — identity + the "how did this
 * data get here" data-path ribbon. Deliberately split out of <SingleMinerView>
 * so it can be tucked into the MinerViewFrame "Details" modal instead of
 * crowding the actual view.
 */
import { ReactNode } from "react";

import type { DataPathStep, SingleMinerSnapshot } from "./types";

function IdentityRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex justify-between gap-4 border-t border-border-5 py-1.5 first:border-t-0">
      <span className="text-200 text-text-primary-50">{label}</span>
      <span className="text-200 text-text-primary">{value}</span>
    </div>
  );
}

function DataPathRibbon({ steps, source, note }: { steps: DataPathStep[]; source: string; note?: string }) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border-5 bg-surface-5 p-3">
      <div className="text-heading-100 tracking-wide text-text-primary-50 uppercase">Data path — {source}</div>
      <div className="flex flex-wrap items-center gap-2">
        {steps.map((step, i) => (
          <div key={`${step.label}-${i}`} className="flex items-center gap-2">
            <div className="rounded-md bg-surface-elevated-base px-2.5 py-1.5">
              <div className="text-200 text-text-primary">{step.label}</div>
              {step.detail ? <div className="text-heading-100 text-text-primary-50">{step.detail}</div> : null}
            </div>
            {i < steps.length - 1 ? <span className="text-text-primary-30">→</span> : null}
          </div>
        ))}
      </div>
      {note ? <div className="text-[10px] text-text-warning italic">{note}</div> : null}
    </div>
  );
}

export function SingleMinerDetails({ snapshot }: { snapshot: SingleMinerSnapshot }) {
  const { identity } = snapshot;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col">
        <div className="mb-1 text-heading-100 tracking-wide text-text-primary-50 uppercase">Identity</div>
        <IdentityRow label="Name" value={identity.name} />
        <IdentityRow label="Model" value={identity.model} />
        <IdentityRow label="Firmware" value={identity.firmware} />
        <IdentityRow label="MDK" value={identity.mdkVersion} />
        <IdentityRow label="MAC" value={identity.macAddress} />
        <IdentityRow label="Serial" value={identity.serialNumber} />
        {identity.ipAddress ? <IdentityRow label="IP" value={identity.ipAddress} /> : null}
      </div>
      <DataPathRibbon steps={snapshot.dataPath} source={snapshot.source} note={snapshot.dataPathNote} />
    </div>
  );
}
