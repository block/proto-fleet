import { type ReactNode, useMemo, useState } from "react";
import { Alert } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import List from "@/shared/components/List";
import Modal from "@/shared/components/Modal";
import Search from "@/shared/components/Search";

export interface UnlocatedHashboard {
  id: string;
  identity: string;
  serial?: string;
  firmware?: string;
  readingsUnavailable?: boolean;
  status: string;
  hashrate: string;
  temperature: string;
  power?: string;
  efficiency?: string;
  issues?: string[];
}

const PAGE_SIZE = 25;

/** App-neutral inventory presentation. Identity never implies a physical slot. */
export default function UnlocatedHashboards({
  boards,
  count = boards.length,
  stale = false,
  renderCallout,
}: {
  boards: UnlocatedHashboard[];
  count?: number;
  stale?: boolean;
  renderCallout?: (callout: ReactNode, onView: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const [selectedId, setSelectedId] = useState<string>();
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    return boards
      .filter((board) => `${board.identity} ${board.serial ?? ""}`.toLowerCase().includes(query))
      .sort((a, b) => a.identity.localeCompare(b.identity));
  }, [boards, search]);
  const lastPage = Math.max(0, Math.ceil(filtered.length / PAGE_SIZE) - 1);
  const currentPage = Math.min(page, lastPage);
  const start = currentPage * PAGE_SIZE;
  const visible = filtered.slice(start, start + PAGE_SIZE);
  const selected = boards.find((board) => board.id === selectedId);
  const onView = () => {
    setOpen(true);
    setSearch("");
    setPage(0);
    setSelectedId(undefined);
  };
  const callout =
    count > 0 ? (
      <Callout
        intent="warning"
        prefixIcon={<Alert />}
        title={`Unlocated hashboards (${count})`}
        subtitle={
          stale ? "Location information may be out of date." : "These hashboards have no reported physical location."
        }
        buttonText="View hashboards"
        buttonOnClick={onView}
      />
    ) : null;
  return (
    <>
      {renderCallout ? renderCallout(callout, onView) : callout}
      {open ? (
        <Modal
          size="large"
          open
          manageFocus
          navigationKey={selected ? selected.id : "list"}
          title="Unlocated hashboards"
          dialogLabel="Unlocated hashboards"
          onDismiss={() => setOpen(false)}
          bodyClassName="text-text-primary"
          fixedFooter={
            !selected ? (
              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border-5 pt-4">
                <p role="status">
                  {filtered.length
                    ? `Showing ${start + 1}–${start + visible.length} of ${filtered.length}`
                    : "No matching hashboards."}
                </p>
                {lastPage > 0 ? (
                  <div className="flex gap-3">
                    <Button
                      variant="secondary"
                      size="compact"
                      text="Previous"
                      disabled={currentPage === 0}
                      onClick={() => {
                        setPage(currentPage - 1);
                        setSelectedId(undefined);
                      }}
                    />
                    <Button
                      variant="secondary"
                      size="compact"
                      text="Next"
                      disabled={currentPage === lastPage}
                      onClick={() => {
                        setPage(currentPage + 1);
                        setSelectedId(undefined);
                      }}
                    />
                  </div>
                ) : null}
              </div>
            ) : undefined
          }
        >
          {stale ? (
            <p role="status" className="mb-4">
              Location information may be out of date. Current readings are unavailable.
            </p>
          ) : null}
          {!selected ? (
            <>
              <div className="mb-4">
                <Search
                  id="unlocated-hashboard-search"
                  label="Search MAC or serial number"
                  initValue={search}
                  onChange={(value) => {
                    setSearch(value);
                    setPage(0);
                    setSelectedId(undefined);
                  }}
                />
              </div>
              <List<UnlocatedHashboard, string, "identity" | "status" | "hashrate" | "temperature" | "issues">
                items={visible}
                itemKey="id"
                itemSelectable={false}
                hideTotal
                activeCols={["identity", "status", "hashrate", "temperature", "issues"]}
                colTitles={{
                  identity: "Hashboard",
                  status: "Status",
                  hashrate: "Hashrate",
                  temperature: "Temperature",
                  issues: "Issues",
                }}
                stickyBgColor="bg-surface-elevated-base"
                colConfig={{
                  identity: {
                    width: "w-48",
                    allowWrap: true,
                    component: (board) => (
                      <Button
                        variant="textOnly"
                        text={board.identity}
                        ariaLabel={`View hashboard ${board.identity}`}
                        onClick={() => setSelectedId(board.id)}
                      />
                    ),
                  },
                  status: {
                    width: "w-36",
                    component: (board) => (stale || board.readingsUnavailable ? "Unknown" : board.status),
                  },
                  hashrate: {
                    width: "w-32",
                    component: (board) => (stale || board.readingsUnavailable ? "—" : board.hashrate),
                  },
                  temperature: {
                    width: "w-32",
                    component: (board) => (stale || board.readingsUnavailable ? "—" : board.temperature),
                  },
                  issues: {
                    width: "w-24",
                    component: (board) =>
                      stale || board.readingsUnavailable || board.issues === undefined
                        ? "—"
                        : String(board.issues.length),
                  },
                }}
                noDataElement={
                  <p className="py-6">
                    {boards.length
                      ? "No matching hashboards."
                      : count
                        ? "Hashboard details are unavailable. Refresh the container to try again."
                        : "No unlocated hashboards reported."}
                  </p>
                }
              />
            </>
          ) : null}
          {selected ? (
            <section
              aria-label={`Hashboard ${selected.identity}`}
              className="mt-6 space-y-3 border-t border-border-5 pt-4"
            >
              <Button variant="textOnly" text="Back to hashboards" onClick={() => setSelectedId(undefined)} />
              <h3 className="text-emphasis-300">{selected.identity}</h3>
              <p>Location unknown</p>
              {!stale && selected.readingsUnavailable ? <p role="status">Current readings are unavailable.</p> : null}
              <p>Hashrate: {stale || selected.readingsUnavailable ? "—" : selected.hashrate}</p>
              <p>Temperature: {stale || selected.readingsUnavailable ? "—" : selected.temperature}</p>
              {selected.power ? <p>Power: {stale || selected.readingsUnavailable ? "—" : selected.power}</p> : null}
              {selected.efficiency ? (
                <p>Efficiency: {stale || selected.readingsUnavailable ? "—" : selected.efficiency}</p>
              ) : null}
              {selected.serial ? <p>Serial number: {selected.serial}</p> : null}
              {selected.firmware ? <p>Firmware: {selected.firmware}</p> : null}
              {stale || selected.readingsUnavailable || selected.issues === undefined ? (
                <p>Issue details unavailable.</p>
              ) : selected.issues.length ? (
                <ul className="list-disc pl-5">
                  {selected.issues.map((issue, index) => (
                    <li key={index}>{issue}</li>
                  ))}
                </ul>
              ) : (
                <p>No reported issues.</p>
              )}
            </section>
          ) : null}
        </Modal>
      ) : null}
    </>
  );
}
