import { type ReactNode, useState } from "react";

import Button from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { copyToClipboard } from "@/shared/utils/utility";

export const SecretModal = ({
  title,
  label,
  value,
  description,
  helperText,
  onDismiss,
  children,
}: {
  title: string;
  label: string;
  value: string;
  description: string;
  helperText?: string;
  onDismiss: () => void;
  children?: ReactNode;
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
        {helperText ? <p className="text-200 text-text-primary-70">{helperText}</p> : null}
        <Button
          variant="secondary"
          text={copied ? "Copied" : `Copy ${label.toLowerCase()}`}
          onClick={() => {
            void copyToClipboard(value).then(
              () => {
                setCopied(true);
                setError("");
              },
              () => setError("Couldn’t copy. Select and copy the value above."),
            );
          }}
        />
        {error ? (
          <p role="alert" className="text-text-critical">
            {error}
          </p>
        ) : null}
        {children}
      </div>
    </Modal>
  );
};
