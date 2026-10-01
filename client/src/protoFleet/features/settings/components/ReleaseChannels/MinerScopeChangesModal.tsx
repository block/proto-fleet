import { useId, useLayoutEffect, useMemo, useRef, useState } from "react";

import type { MinerScopeChanges } from "./channelSettingsChangesUtils";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal, { sizes } from "@/shared/components/Modal";

const PAGE_SIZE = 50;

export default function MinerScopeChangesModal({
  changes,
  onClose,
}: {
  changes: MinerScopeChanges;
  onClose: () => void;
}) {
  const searchId = useId();
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const tableContainer = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (tableContainer.current) tableContainer.current.scrollTop = 0;
  }, [page, search]);
  const miners = useMemo(() => {
    const query = search.trim().toLocaleLowerCase();
    return changes.miners.filter(
      (miner) => miner.name.toLocaleLowerCase().includes(query) || miner.identifier.toLocaleLowerCase().includes(query),
    );
  }, [changes.miners, search]);
  const start = page * PAGE_SIZE;
  const end = Math.min(start + PAGE_SIZE, miners.length);

  return (
    <Modal
      title="Miner changes"
      description="Compare the miners selected directly in Applies to."
      size={sizes.large}
      zIndex="z-70"
      testId="miner-scope-changes-modal"
      onDismiss={onClose}
      buttons={[{ text: "Done", variant: variants.primary }]}
    >
      <div className="grid gap-4">
        <Input
          id={searchId}
          label="Search miners"
          autoFocus
          onKeyDown={(key) => {
            if (key === "Escape") onClose();
          }}
          onChange={(value) => {
            setSearch(value);
            setPage(0);
          }}
        />
        <div ref={tableContainer} className="max-h-[50dvh] overflow-y-auto">
          <table aria-label="Miner changes" className="w-full table-fixed text-left text-200">
            <thead className="sticky top-0 bg-surface-elevated-base text-text-primary-50">
              <tr className="border-b border-border-5">
                <th scope="col" className="w-1/2 py-2 pr-3 font-normal">
                  Miner
                </th>
                <th scope="col" className="py-2 pr-3 font-normal">
                  Original
                </th>
                <th scope="col" className="py-2 font-normal">
                  Target
                </th>
              </tr>
            </thead>
            <tbody>
              {miners.slice(start, end).map((miner) => (
                <tr key={miner.identifier} className="border-b border-border-5">
                  <th scope="row" className="py-2 pr-3 font-normal wrap-anywhere">
                    <span className="block">{miner.name}</span>
                    {miner.name !== miner.identifier ? (
                      <span className="block text-text-primary-50">{miner.identifier}</span>
                    ) : null}
                  </th>
                  <td className="py-2 pr-3">{miner.original ? "Included" : "Not included"}</td>
                  <td className="py-2">{miner.target ? "Included" : "Not included"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {miners.length === 0 ? <p className="text-200 text-text-primary-70">No miners match your search.</p> : null}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p role="status" className="text-200 text-text-primary-70">
            {miners.length > 0 ? `${(start + 1).toLocaleString()}–${end.toLocaleString()} of ` : ""}
            {miners.length.toLocaleString()} {miners.length === 1 ? "miner" : "miners"}
          </p>
          {miners.length > PAGE_SIZE ? (
            <div className="flex gap-2">
              <Button
                text="Previous"
                variant={variants.secondary}
                size={buttonSizes.compact}
                disabled={page === 0}
                onClick={() => setPage(page - 1)}
              />
              <Button
                text="Next"
                variant={variants.secondary}
                size={buttonSizes.compact}
                disabled={end === miners.length}
                onClick={() => setPage(page + 1)}
              />
            </div>
          ) : null}
        </div>
      </div>
    </Modal>
  );
}
