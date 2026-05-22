import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface SearchRacksModalProps {
  open: boolean;
  // Parent building context drives the eligibility split: racks under the
  // same site whose building_id equals this building are eligible to
  // reposition (or leave unchanged); racks in a different building of the
  // same site render greyed out (ineligible-but-visible — matches the
  // SearchMinersModal pattern).
  siteId: bigint;
  currentBuildingId: bigint;
  // Racks already in the modal's working set — rendered as "Already added"
  // and not selectable so the operator can't double-add the same rack.
  alreadyAddedRackIds?: bigint[];
  onDismiss: () => void;
  onConfirm: (rackId: bigint, label: string) => void;
}

// Rack row shape used by the picker. We extract just what the list needs so
// the component doesn't drag the full DeviceSet/RackInfo discriminator into
// its render path.
interface RackRow {
  id: bigint;
  label: string;
  buildingId?: bigint;
  // Disabled when the rack lives in a different building of the same site
  // (ineligible-but-visible) OR is already in the modal's working set.
  disabled: boolean;
  reason: "inOtherBuilding" | "alreadyAdded" | "inThisBuilding" | "unassigned";
}

const buildRow = (rack: DeviceSet, currentBuildingId: bigint, alreadyAdded: Set<string>): RackRow | null => {
  if (rack.typeDetails.case !== "rackInfo") return null;
  const info = rack.typeDetails.value;
  const buildingId = info.buildingId;
  const inOtherBuilding = buildingId !== undefined && buildingId !== 0n && buildingId !== currentBuildingId;
  const inThisBuilding = buildingId === currentBuildingId;
  const isAlreadyAdded = alreadyAdded.has(rack.id.toString());
  const disabled = inOtherBuilding || isAlreadyAdded;
  const reason: RackRow["reason"] = inOtherBuilding
    ? "inOtherBuilding"
    : isAlreadyAdded
      ? "alreadyAdded"
      : inThisBuilding
        ? "inThisBuilding"
        : "unassigned";
  return { id: rack.id, label: rack.label, buildingId, disabled, reason };
};

const SearchRacksModal = ({
  open,
  siteId,
  currentBuildingId,
  alreadyAddedRackIds,
  onDismiss,
  onConfirm,
}: SearchRacksModalProps) => {
  const { listRacks } = useDeviceSets();

  const [rows, setRows] = useState<RackRow[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<bigint | null>(null);

  // Set is rebuilt on every change so the effect deps stay primitive. Using
  // .toString() because bigint isn't a valid Set key for stable membership.
  const alreadyAddedSet = useMemo(() => {
    const s = new Set<string>();
    for (const id of alreadyAddedRackIds ?? []) s.add(id.toString());
    return s;
  }, [alreadyAddedRackIds]);

  // Server doesn't yet support a site_ids filter on ListDeviceSets (Phase 1b
  // backend follow-up). For PR 3 we fetch the full rack list unpaginated and
  // narrow client-side to the parent site. Acceptable while fleets are small;
  // the moment ListDeviceSets gains site filtering this collapses into a
  // server-side filter call.
  // Conditional render in the host means each open creates a fresh mount, so
  // the initial useState values cover the "reset on open" case. The effect
  // only fires the network call — no synchronous setState — which keeps the
  // react-hooks/set-state-in-effect rule happy.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void listRacks({
      onSuccess: (racks) => {
        if (cancelled) return;
        const filtered: RackRow[] = [];
        for (const rack of racks) {
          if (rack.typeDetails.case !== "rackInfo") continue;
          if (rack.typeDetails.value.siteId !== siteId) continue;
          const row = buildRow(rack, currentBuildingId, alreadyAddedSet);
          if (row) filtered.push(row);
        }
        filtered.sort((a, b) => a.label.localeCompare(b.label));
        setRows(filtered);
      },
      onError: (msg) => {
        if (cancelled) return;
        setError(msg);
        setRows([]);
      },
    });
    return () => {
      cancelled = true;
    };
  }, [open, siteId, currentBuildingId, alreadyAddedSet, listRacks]);

  const filtered = useMemo(() => {
    if (!rows) return undefined;
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) => r.label.toLowerCase().includes(q));
  }, [rows, query]);

  const handleConfirm = useCallback(() => {
    if (selected === null) return;
    const row = rows?.find((r) => r.id === selected);
    if (!row) return;
    onConfirm(selected, row.label);
  }, [selected, rows, onConfirm]);

  return (
    <Modal
      open={open}
      title="Search racks"
      size="large"
      onDismiss={onDismiss}
      divider={false}
      testId="search-racks-modal"
      buttons={[
        {
          text: "Assign",
          variant: "primary",
          disabled: selected === null,
          onClick: handleConfirm,
          dismissModalOnClick: false,
          testId: "search-racks-modal-confirm",
        },
      ]}
    >
      <div className="flex flex-col gap-4 py-2">
        <Input
          id="search-racks-query"
          label="Search by rack label"
          initValue={query}
          onChange={setQuery}
          testId="search-racks-query-input"
        />
        {error ? (
          <div className="text-300 text-intent-critical-fill" data-testid="search-racks-modal-error">
            {error}
          </div>
        ) : filtered === undefined ? (
          <div className="flex items-center justify-center py-8">
            <ProgressCircular indeterminate />
          </div>
        ) : filtered.length === 0 ? (
          <div className="py-6 text-center text-300 text-text-primary-50" data-testid="search-racks-modal-empty">
            {rows && rows.length === 0 ? "No racks in this site yet." : "No racks match your search."}
          </div>
        ) : (
          <ul className="flex max-h-[50vh] flex-col overflow-y-auto" data-testid="search-racks-modal-list">
            {filtered.map((row) => {
              const isSelected = selected === row.id;
              const reasonLabel = {
                inOtherBuilding: "In another building",
                alreadyAdded: "Already added",
                inThisBuilding: "In this building",
                unassigned: "Unassigned",
              }[row.reason];
              return (
                <li key={row.id.toString()}>
                  <button
                    type="button"
                    disabled={row.disabled}
                    onClick={() => setSelected(row.id)}
                    className={clsx(
                      "flex w-full items-center justify-between gap-3 border-b border-border-5 px-3 py-3 text-left",
                      row.disabled
                        ? "cursor-not-allowed opacity-40"
                        : "hover:bg-surface-base-hover focus:bg-surface-base-hover",
                      isSelected && "bg-surface-base-hover",
                    )}
                    data-testid={`search-racks-modal-row-${row.id.toString()}`}
                    aria-disabled={row.disabled || undefined}
                  >
                    <span className="truncate text-emphasis-300">{row.label || "(unnamed rack)"}</span>
                    <span className="shrink-0 text-300 text-text-primary-50">{reasonLabel}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </Modal>
  );
};

export default SearchRacksModal;
