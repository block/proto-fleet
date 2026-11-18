import { useEffect, useState } from "react";

import { ticketAssetKey, ticketAssetLabel, ticketAssetsShareParent } from "../ticketAssets";
import type { TicketAsset } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import type { TicketAssetPage } from "@/protoFleet/api/maintenance";
import Button, { sizes } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

const TicketAssetPicker = ({
  title = "Select affected assets",
  load,
  singleParent = false,
  initialSelected = [],
  onSelect,
  onDismiss,
}: {
  title?: string;
  load: (search: string, signal: AbortSignal, pageToken: string) => Promise<TicketAssetPage>;
  singleParent?: boolean;
  initialSelected?: TicketAsset[];
  onSelect: (assets: TicketAsset[]) => void;
  onDismiss: () => void;
}) => {
  const [search, setSearch] = useState("");
  const [pageTokens, setPageTokens] = useState<string[]>([""]);
  const [nextPageToken, setNextPageToken] = useState("");
  const pageToken = pageTokens[pageTokens.length - 1];
  const [assets, setAssets] = useState<TicketAsset[]>([]);
  const [selected, setSelected] = useState<TicketAsset[]>(initialSelected);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setLoading(true);
      setError("");
      load(search, controller.signal, pageToken).then(
        (page) => {
          if (!controller.signal.aborted) {
            setAssets(page.assets);
            setNextPageToken(page.nextPageToken);
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
  }, [load, search, pageToken, revision]);
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
        <Input
          id="ticket-asset-search"
          label="Search assets"
          initValue={search}
          maxLength={255}
          onChange={(value) => {
            setLoading(true);
            setSearch(value);
            setPageTokens([""]);
          }}
        />
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
              const incompatible =
                first &&
                (singleParent
                  ? !ticketAssetsShareParent(asset, first)
                  : asset.kind !== first.kind || asset.siteId !== first.siteId);
              return (
                <label key={ticketAssetKey(asset)} className="flex items-start gap-3 text-300">
                  <Checkbox
                    checked={checked}
                    disabled={!!incompatible || (!checked && selected.length >= 100)}
                    onChange={() =>
                      setSelected((items) =>
                        checked
                          ? items.filter((item) => ticketAssetKey(item) !== ticketAssetKey(asset))
                          : [...items, asset],
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
        <div className="flex items-center justify-center gap-3">
          <Button
            text="Previous"
            ariaLabel="Previous page"
            variant="secondary"
            size={sizes.compact}
            disabled={loading || pageTokens.length === 1}
            onClick={() => {
              setLoading(true);
              setPageTokens((tokens) => tokens.slice(0, -1));
            }}
          />
          <span className="text-300">Page {pageTokens.length}</span>
          <Button
            text="Next"
            ariaLabel="Next page"
            variant="secondary"
            size={sizes.compact}
            disabled={loading || !!error || !nextPageToken}
            onClick={() => {
              setLoading(true);
              setPageTokens((tokens) => [...tokens, nextPageToken]);
            }}
          />
        </div>
        <p className="text-200 text-text-primary-50">
          {singleParent
            ? "Select one miner or one module with its affected hashboards."
            : "Select one equipment type at one site. Search to narrow the results."}
        </p>
      </div>
    </Modal>
  );
};
export default TicketAssetPicker;
