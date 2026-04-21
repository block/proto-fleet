import SwitchComponent from ".";

interface SwitchArgs {
  label?: string;
  disabled?: boolean;
}

export const Switch = ({ label, disabled }: SwitchArgs) => {
  return <SwitchComponent label={label} disabled={disabled} />;
};

export default {
  title: "Shared/Switch",
  args: {
    label: "Show passwords",
    disabled: false,
  },
};
