import DialogComponent from ".";

export const Dialog = () => {
  return (
    <DialogComponent
      title="Connecting to your mining pool"
      subtitle="This may take a few seconds"
      loading
      show
    />
  );
};

export default {
  title: "Dialog",
};
