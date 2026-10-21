import { useState } from "react";
import { SecretModal } from "./SecretModal";
import Button from "@/shared/components/Button";
import { copyToClipboard } from "@/shared/utils/utility";

export const ShareInvitationModal = ({ token, onDismiss }: { token: string; onDismiss: () => void }) => {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const link = new URL("/account-invitation", window.location.origin);
  link.hash = new URLSearchParams({ token }).toString();
  return (
    <SecretModal
      title="Share invitation"
      label="Invitation code"
      value={token}
      onDismiss={onDismiss}
      description="Send this code to the person you’re inviting. They can enter it in Fleet under Join an account."
      helperText="Copy this code before closing. You won’t be able to view it again."
    >
      <p className="text-200 text-text-primary-50">
        The link works only if the recipient can reach {window.location.origin}.
      </p>
      <Button
        variant="textOnly"
        text={copied ? "Link copied" : "Copy link"}
        onClick={() => {
          void copyToClipboard(link.toString()).then(
            () => {
              setCopied(true);
              setError("");
            },
            () => setError("Couldn’t copy the link. Share the code instead."),
          );
        }}
      />
      {error ? (
        <p role="alert" className="text-text-critical">
          {error}
        </p>
      ) : null}
    </SecretModal>
  );
};
