import { useState } from "react";

import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import Checkbox from "@/shared/components/Checkbox";
import Modal from "@/shared/components/Modal";
import ModalSelectAllFooter from "@/shared/components/Modal/ModalSelectAllFooter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";

export interface ContainerSelectionModalProps {
  // The readable container catalog, owned by the caller so the scope editor
  // and this picker agree on which identifiers are containers.
  containers: Container[];
  loading: boolean;
  hasLoadError: boolean;
  // Device identifiers already selected. Identifiers that are not listed
  // containers are ignored here and never returned.
  selectedIdentifiers: string[];
  onDismiss: () => void;
  onSave: (containerIdentifiers: string[]) => void;
}

const ContainerSelectionModal = ({
  containers,
  loading,
  hasLoadError,
  selectedIdentifiers,
  onDismiss,
  onSave,
}: ContainerSelectionModalProps) => {
  const [draftSelection, setDraftSelection] = useState(() => new Set(selectedIdentifiers));
  const selected = containers.filter((container) => draftSelection.has(container.deviceIdentifier));
  const allSelected = containers.length > 0 && selected.length === containers.length;
  const showEmptyState = !loading && !hasLoadError && containers.length === 0;
  const selectAll = () =>
    setDraftSelection((current) => new Set([...current, ...containers.map((container) => container.deviceIdentifier)]));
  const toggle = (identifier: string) =>
    setDraftSelection((current) => {
      const next = new Set(current);
      if (!next.delete(identifier)) next.add(identifier);
      return next;
    });

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={
        hasLoadError ? "Couldn't load containers" : showEmptyState ? "No containers available" : "Select containers"
      }
      divider={false}
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: () => onSave(selected.map((container) => container.deviceIdentifier)),
          dismissModalOnClick: false,
          disabled: loading || hasLoadError,
        },
      ]}
    >
      {loading ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : hasLoadError ? (
        <div className="text-300 text-text-primary-70">
          Couldn&apos;t load containers. Close this modal and try again.
        </div>
      ) : showEmptyState ? (
        <div className="text-300 text-text-primary-70">Pair a container to target it directly.</div>
      ) : (
        <div className="flex flex-col">
          <Row divider>
            <label className="flex w-full cursor-pointer items-center gap-4">
              <Checkbox
                checked={allSelected}
                partiallyChecked={!allSelected ? selected.length > 0 : false}
                onChange={() => (allSelected ? setDraftSelection(new Set()) : selectAll())}
              />
              <span className="text-emphasis-300 text-text-primary">All containers</span>
            </label>
          </Row>

          {containers.map((container) => (
            <Row key={container.deviceIdentifier} divider={false} compact>
              <label className="flex w-full cursor-pointer items-center gap-4">
                <Checkbox
                  checked={draftSelection.has(container.deviceIdentifier)}
                  onChange={() => toggle(container.deviceIdentifier)}
                />
                <div className="flex flex-col">
                  <span className="text-emphasis-300 text-text-primary">
                    {container.name || container.deviceIdentifier}
                  </span>
                  <span className="text-200 text-text-primary-70">{container.site?.label || "Unassigned"}</span>
                </div>
              </label>
            </Row>
          ))}

          <ModalSelectAllFooter
            label={`${selected.length} ${selected.length === 1 ? "container" : "containers"} selected`}
            onSelectAll={selectAll}
            onSelectNone={() => setDraftSelection(new Set())}
          />
        </div>
      )}
    </Modal>
  );
};

export default ContainerSelectionModal;
