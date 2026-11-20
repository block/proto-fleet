import { useCallback, useMemo, useState } from "react";
import type { PoolBoardAssignment } from "@/protoOS/api/adapters/poolConfiguration";
import { macKey } from "@/protoOS/utils/macAddress";
import { ChevronDown } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import Search from "@/shared/components/Search";

const PAGE_SIZE = 25;
const searchText = (value: string) => value.toLowerCase().replaceAll("·", " ").trim().replace(/\s+/g, " ");

interface PoolHashboardsProps {
  boards: PoolBoardAssignment[];
  title: string;
  assignmentBlockers?: ReadonlyMap<string, string>;
  onEditPool?: (macs: string[]) => void;
  poolPickerOpen?: boolean;
}

function HashboardsModal({
  boards,
  title,
  assignmentBlockers,
  onDismiss,
  onEditPool,
  poolPickerOpen,
}: PoolHashboardsProps & { onDismiss: () => void }) {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const [selection, setSelection] = useState<string[]>([]);
  const sortedBoards = useMemo(
    () =>
      [...boards].sort((a, b) => {
        if (!a.location) return b.location ? 1 : a.mac.localeCompare(b.mac);
        if (!b.location) return -1;
        return a.location.localeCompare(b.location, undefined, { numeric: true }) || a.mac.localeCompare(b.mac);
      }),
    [boards],
  );
  const filteredBoards = useMemo(() => {
    const query = searchText(search);
    return sortedBoards.filter((board) => {
      const location = searchText(board.location ?? "Location unavailable");
      return location.includes(query) || board.mac.toLowerCase().includes(query);
    });
  }, [sortedBoards, search]);
  const matchCount = filteredBoards.length;
  const lastPage = Math.max(0, Math.ceil(matchCount / PAGE_SIZE) - 1);
  const currentPage = Math.min(page, lastPage);
  const start = currentPage * PAGE_SIZE;
  const visibleBoards = filteredBoards.slice(start, start + PAGE_SIZE);
  const selectableMacs = useMemo(
    () => new Set(boards.map(({ mac }) => mac).filter((mac) => !assignmentBlockers?.has(macKey(mac)))),
    [boards, assignmentBlockers],
  );
  const canSelect = useCallback(({ mac }: PoolBoardAssignment) => selectableMacs.has(mac), [selectableMacs]);
  const selectableMatches = filteredBoards.filter(canSelect);
  const selected = useMemo(() => {
    return selection.filter((mac) => selectableMacs.has(mac));
  }, [selection, selectableMacs]);

  return (
    <Modal
      open={!poolPickerOpen}
      manageFocus
      title={title}
      dialogLabel={title}
      onDismiss={onDismiss}
      bodyClassName="text-text-primary"
      fixedFooter={
        matchCount || onEditPool ? (
          <>
            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border-5 pt-4">
              {matchCount ? (
                <p role="status" className="text-200 text-text-primary-70">
                  Showing {start + 1}–{start + visibleBoards.length} of {matchCount}{" "}
                  {matchCount === 1 ? "hashboard" : "hashboards"}
                </p>
              ) : null}
              {lastPage > 0 ? (
                <div className="flex gap-3">
                  <Button
                    variant="secondary"
                    size="compact"
                    ariaLabel="Previous page"
                    prefixIcon={<ChevronDown className="rotate-90" />}
                    disabled={currentPage === 0}
                    onClick={() => setPage(currentPage - 1)}
                  />
                  <Button
                    variant="secondary"
                    size="compact"
                    ariaLabel="Next page"
                    prefixIcon={<ChevronDown className="rotate-270" />}
                    disabled={currentPage === lastPage}
                    onClick={() => setPage(currentPage + 1)}
                  />
                </div>
              ) : null}
              {onEditPool ? (
                <Button
                  variant="secondary"
                  size="compact"
                  text="Edit pool"
                  disabled={!selected.length}
                  className="ml-auto"
                  onClick={() => onEditPool(selected)}
                />
              ) : null}
            </div>
            {onEditPool ? (
              <div className="pt-3">
                <ModalSelectAllFooter
                  label={`${selected.length} ${selected.length === 1 ? "hashboard" : "hashboards"} selected`}
                  onSelectAll={() => setSelection(selectableMatches.map(({ mac }) => mac))}
                  selectAllDisabled={!selectableMatches.length}
                  onSelectNone={() => setSelection([])}
                />
              </div>
            ) : null}
          </>
        ) : undefined
      }
    >
      <div className="mb-4">
        <Search
          id="pool-hashboard-search"
          label="Search MAC or location"
          initValue={search}
          onChange={(value) => {
            setSearch(value);
            setPage(0);
          }}
        />
      </div>
      <List<PoolBoardAssignment, string, "location" | "mac">
        items={visibleBoards}
        itemKey="mac"
        itemName={{ singular: "hashboard", plural: "hashboards on this page" }}
        itemSelectable={Boolean(onEditPool)}
        isRowSelectable={canSelect}
        preserveOffPageSelection
        customSelectedItems={selected}
        customSetSelectedItems={setSelection}
        activeCols={["location", "mac"]}
        colTitles={{ location: "Location", mac: "MAC address" }}
        colConfig={{
          location: {
            width: "w-40 tablet:w-72",
            allowWrap: true,
            component: (board) => board.location ?? "Location unavailable",
          },
          mac: {
            width: "w-40 tablet:w-48",
            allowWrap: true,
            component: ({ mac }) => {
              const blocker = assignmentBlockers?.get(macKey(mac));
              return blocker ? (
                <div className="flex flex-col gap-1">
                  <span>{mac}</span>
                  <span className="text-200 text-text-primary-70">{blocker}</span>
                </div>
              ) : (
                mac
              );
            },
          },
        }}
        hideTotal
        noDataElement={<p className="py-6">No matching hashboards.</p>}
      />
    </Modal>
  );
}

export default function PoolHashboards({
  countLabel = "Hashboards",
  ...props
}: PoolHashboardsProps & { countLabel?: string }) {
  const { boards, title } = props;
  const [open, setOpen] = useState(false);
  return (
    <>
      <div className="flex items-center justify-between gap-4">
        <p className="text-300 text-text-primary">
          {countLabel}: {boards.length}
        </p>
        {boards.length ? (
          <Button variant="textOnly" text="View hashboards" ariaLabel={`View ${title}`} onClick={() => setOpen(true)} />
        ) : null}
      </div>
      {open ? <HashboardsModal {...props} onDismiss={() => setOpen(false)} /> : null}
    </>
  );
}

export function UnassignedHashboards(props: Omit<PoolHashboardsProps, "title">) {
  if (!props.boards.length) return null;

  return (
    <section aria-label="No reported group assignment" className="space-y-3">
      <h3 className="text-emphasis-300">No reported group assignment</h3>
      <PoolHashboards {...props} title="Hashboards with no reported group assignment" />
    </section>
  );
}
