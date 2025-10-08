import AnimatedDotsBackground from ".";

interface ButtonProps {
  connecting?: boolean;
}

export const AnimatedDots = ({ connecting }: ButtonProps) => {
  return (
    <div className="flex flex-col space-y-4">
      <div className="flex h-svh w-full space-x-2">
        <AnimatedDotsBackground connecting={connecting} />
      </div>
    </div>
  );
};

AnimatedDots.args = {
  connecting: false,
};
AnimatedDots.argTypes = {
  size: {
    control: "",
    options: [true, false],
  },
};

export default {
  title: "Shared/Animation",
};
