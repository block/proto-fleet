import { useMemo, useState } from "react";
import type { PoolBoardAssignment } from "@/protoOS/api/adapters/poolConfiguration";
import { ChevronDown } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import Modal from "@/shared/components/Modal";
import Search from "@/shared/components/Search";

const PAGE_SIZE = 25;
const searchText = (value: string) => value.toLowerCase().replaceAll("·", " ").trim().replace(/\s+/g, " ");

interface PoolHashboardsProps {
  boards: PoolBoardAssignment[];
  title: string;
  countLabel?: string;
}

function HashboardsModal({ boards, title, onDismiss }: PoolHashboardsProps & { onDismiss: () => void }) {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
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
  const lastPage = Math.max(0, Math.ceil(filteredBoards.length / PAGE_SIZE) - 1);
  const currentPage = Math.min(page, lastPage);
  const start = currentPage * PAGE_SIZE;
  const visibleBoards = filteredBoards.slice(start, start + PAGE_SIZE);

  return (
    <Modal
      open
      manageFocus
      title={title}
      dialogLabel={title}
      onDismiss={onDismiss}
      bodyClassName="text-text-primary"
      fixedFooter={
        filteredBoards.length ? (
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border-5 pt-4">
            <p role="status" className="text-200 text-text-primary-70">
              Showing {start + 1}–{start + visibleBoards.length} of {filteredBoards.length}{" "}
              {filteredBoards.length === 1 ? "hashboard" : "hashboards"}
            </p>
            {filteredBoards.length > PAGE_SIZE ? (
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
          </div>
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
      <List<PoolBoardAssignment, string>
        items={visibleBoards}
        itemKey="mac"
        activeCols={["location", "mac"]}
        colTitles={{ location: "Location", mac: "MAC address" }}
        colConfig={{
          location: {
            width: "w-40 tablet:w-72",
            allowWrap: true,
            component: (board) => board.location ?? "Location unavailable",
          },
          mac: { width: "w-40 tablet:w-48", allowWrap: true },
        }}
        hideTotal
        noDataElement={<p className="py-6">No matching hashboards.</p>}
      />
    </Modal>
  );
}

export default function PoolHashboards({ boards, title, countLabel = "Hashboards" }: PoolHashboardsProps) {
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
      {open ? <HashboardsModal boards={boards} title={title} onDismiss={() => setOpen(false)} /> : null}
    </>
  );
}
