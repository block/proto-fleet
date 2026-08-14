import { copyInstallCommand } from "@/protoFleet/features/updates/copyInstallCommand";
import { Copy, LogoAlt } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import type { ButtonProps } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";

interface ManualInstallModalProps {
  installCommand: string;
  onDismiss: () => void;
  open: boolean;
  version: string;
}

const ManualInstallModal = ({ installCommand, onDismiss, open, version }: ManualInstallModalProps) => {
  const buttons: ButtonProps[] = [
    {
      text: "Close",
      variant: variants.secondary,
      onClick: onDismiss,
    },
    {
      ariaLabel: "Copy install command",
      text: "Copy command",
      prefixIcon: <Copy width="w-4" />,
      variant: variants.primary,
      onClick: () => copyInstallCommand(installCommand),
    },
  ];

  return (
    <Dialog
      open={open}
      onDismiss={onDismiss}
      testId="manual-install-modal"
      icon={<LogoAlt width="w-5" />}
      title="Install Fleet manually"
      buttons={buttons}
    >
      <div className="flex flex-col gap-3">
        <p className="text-300 text-text-primary-70">Run this command on the Fleet host to install {version}.</p>
        <pre className="overflow-x-auto rounded-xl bg-core-primary-5 px-4 py-3 font-mono text-200 break-all whitespace-pre-wrap text-text-primary">
          <code>{installCommand}</code>
        </pre>
      </div>
    </Dialog>
  );
};

export default ManualInstallModal;
