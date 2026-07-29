import type { ReleaseInfo } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { copyInstallCommand } from "@/protoFleet/features/updates/copyInstallCommand";
import { Copy } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface UpdateNotificationModalProps {
  installCommand: string;
  onDismiss: () => void;
  open: boolean;
  release?: ReleaseInfo;
}

const UpdateNotificationModal = ({ installCommand, onDismiss, open, release }: UpdateNotificationModalProps) => {
  if (!release) {
    return null;
  }

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={`Update to Fleet ${release.version}`}
      divider={false}
      testId="update-modal"
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-3">
          <p className="text-300 text-text-primary-70">
            Run this command on the host that runs fleetd. It downloads the installer for this release and applies the
            server update.
          </p>
          <div className="flex items-center justify-between gap-2 rounded-xl bg-core-primary-5 px-6 py-6">
            <code className="min-w-0 flex-1 font-mono text-200 break-all text-text-primary">{installCommand}</code>
            <Button
              ariaLabel="Copy install command"
              variant={variants.textOnly}
              size={sizes.textOnly}
              prefixIcon={<Copy width="w-4" />}
              textOnlyUnderlineOnHover={false}
              onClick={() => copyInstallCommand(installCommand)}
              disabled={!installCommand}
              className="shrink-0 text-text-primary hover:!opacity-70"
            />
          </div>
        </div>

        <div className="border-t border-border-5 pt-5">
          <div className="text-heading-100 text-text-primary">Release notes</div>
          {release.releaseNotesUrl ? (
            <a
              href={release.releaseNotesUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-2 inline-block text-300 text-text-primary-70 underline underline-offset-2 hover:text-text-primary"
            >
              View release notes for {release.version}
            </a>
          ) : (
            <p className="mt-2 text-300 text-text-primary-70">No release notes link is available for this release.</p>
          )}
        </div>
      </div>
    </Modal>
  );
};

export default UpdateNotificationModal;
