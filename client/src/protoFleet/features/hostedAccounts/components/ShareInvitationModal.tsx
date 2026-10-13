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
      description="Share this code once. The recipient opens Fleet at their usual address and selects Join an account. The code expires and can be revoked from Invitations."
    >
      <p className="text-200 text-text-primary-50">
        The optional link uses {window.location.origin}. It works only if the recipient can reach that address.
      </p>
      <Button
        variant="textOnly"
        text={copied ? "Link copied" : "Copy invitation link (optional)"}
        onClick={() => {
          void copyToClipboard(link.toString()).then(
            () => {
              setCopied(true);
              setError("");
            },
            () => setError("Copy failed. Share the invitation code instead."),
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
