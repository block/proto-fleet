import Dialog from "@/shared/components/Dialog";

interface WakingDialogProps {
  open?: boolean;
}

const WakingDialog = ({ open }: WakingDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Waking up miner"
      preventScroll
      subtitle="This may take a few seconds."
      subtitleSize="text-300"
      loading
      testId="waking-dialog"
    />
  );
};

export default WakingDialog;
