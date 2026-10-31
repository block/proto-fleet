import { useEffect, useState } from "react";

import { ticketAssetKey, ticketAssetLabel } from "../ticketAssets";
import type { TicketAsset } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import Button, { sizes } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

const TicketAssetPicker = ({
  title = "Select affected assets",
  load,
  multiple = true,
  onSelect,
  onDismiss,
}: {
  title?: string;
  load: (search: string, signal: AbortSignal) => Promise<TicketAsset[]>;
  multiple?: boolean;
  onSelect: (assets: TicketAsset[]) => void;
  onDismiss: () => void;
}) => {
  const [search, setSearch] = useState("");
  const [assets, setAssets] = useState<TicketAsset[]>([]);
  const [selected, setSelected] = useState<TicketAsset[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setLoading(true);
      setError("");
      load(search, controller.signal).then(
        (items) => {
          if (!controller.signal.aborted) {
            setAssets(items);
            setLoading(false);
          }
        },
        (cause: unknown) => {
          if (!controller.signal.aborted) {
            setAssets([]);
            setError(getErrorMessage(cause));
            setLoading(false);
          }
        },
      );
    }, 200);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [load, search, revision]);
  const selectedKeys = new Set(selected.map(ticketAssetKey));
  const first = selected[0];
  return (
    <Modal
      open
      manageFocus
      dialogLabel={title}
      title={title}
      onDismiss={onDismiss}
      buttons={[
        { text: "Cancel", variant: "secondary", onClick: onDismiss },
        {
          text: "Select assets",
          variant: "primary",
          disabled: selected.length === 0,
          onClick: () => onSelect(selected),
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <Input id="ticket-asset-search" label="Search assets" initValue={search} maxLength={255} onChange={setSearch} />
        {selected.length ? <p className="text-200 text-text-primary-70">{selected.length} selected</p> : null}
        {loading ? (
          <div role="status" aria-label="Loading assets">
            <ProgressCircular indeterminate />
          </div>
        ) : error ? (
          <div role="alert">
            {error}
            <Button
              text="Retry"
              variant="secondary"
              size={sizes.compact}
              onClick={() => setRevision((value) => value + 1)}
            />
          </div>
        ) : (
          <div className="flex max-h-80 flex-col gap-3 overflow-y-auto">
            {assets.map((asset) => {
              const checked = selectedKeys.has(ticketAssetKey(asset));
              const incompatible = multiple && first && (asset.kind !== first.kind || asset.siteId !== first.siteId);
              return (
                <label key={ticketAssetKey(asset)} className="flex items-start gap-3 text-300">
                  <Checkbox
                    checked={checked}
                    disabled={!!incompatible || (!checked && selected.length >= 100)}
                    onChange={() =>
                      setSelected((items) =>
                        checked
                          ? items.filter((item) => ticketAssetKey(item) !== ticketAssetKey(asset))
                          : multiple
                            ? [...items, asset]
                            : [asset],
                      )
                    }
                  />
                  <span>{ticketAssetLabel(asset)}</span>
                </label>
              );
            })}
            {!assets.length ? <p>No matching assets.</p> : null}
          </div>
        )}
        {multiple ? (
          <p className="text-200 text-text-primary-50">
            Select one equipment type at one site. Search to narrow the results.
          </p>
        ) : null}
      </div>
    </Modal>
  );
};
export default TicketAssetPicker;
