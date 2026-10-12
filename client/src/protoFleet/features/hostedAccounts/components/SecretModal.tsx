import { useState } from "react";

import Button from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";

export const SecretModal = ({
  title,
  label,
  value,
  description,
  onDismiss,
}: {
  title: string;
  label: string;
  value: string;
  description: string;
  onDismiss: () => void;
}) => {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  return (
    <Modal
      open
      title={title}
      dialogLabel={title}
      onDismiss={onDismiss}
      manageFocus
      buttons={[{ text: "Done", variant: "primary", onClick: onDismiss }]}
    >
      <div className="flex flex-col gap-4" data-dd-privacy="mask">
        <p>{description}</p>
        <Input id="hosted-one-time-secret" label={label} initValue={value} readOnly autoComplete="off" />
        <Button
          variant="secondary"
          text={copied ? "Copied" : `Copy ${label.toLowerCase()}`}
          onClick={() => {
            void navigator.clipboard.writeText(value).then(
              () => {
                setCopied(true);
                setError("");
              },
              () => setError("Copy failed. Select and copy the value above."),
            );
          }}
        />
        {error ? (
          <p role="alert" className="text-text-critical">
            {error}
          </p>
        ) : null}
      </div>
    </Modal>
  );
};
