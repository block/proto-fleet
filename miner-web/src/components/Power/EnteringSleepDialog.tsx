import Dialog from "components/Dialog";

interface EnteringSleepDialogProps {
  show: boolean;
}

const EnteringSleepDialog = ({ show }: EnteringSleepDialogProps) => {
  return (
    <Dialog
      title="Entering sleep mode"
      preventScroll
      subtitle="Your miner is entering sleep mode. This may take a few seconds."
      subtitleSize="text-300"
      loading
      show={show}
      testId="entering-sleep-dialog"
    />
  );
};

export default EnteringSleepDialog;
