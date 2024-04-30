import BarComponent from "./Bar";

interface BarProps {
  intensity: number;
}

export const Bar = ({ intensity }: BarProps) => (
  <BarComponent intensity={intensity} />
);

export default {
  title: "Components/Info Widgets/Bar",
  args: {
    intensity: 5,
  },
  argTypes: {
    intensity: { control: { type: "range", min: 0, max: 10, step: 1 } },
  },
};
