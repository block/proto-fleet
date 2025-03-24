import clsx from "clsx";

import { IconProps } from "./types";

type HashboardIndicatorProps = IconProps & {
  activeHashboard?: number;
  totalHashboards?: number;
};

const HashboardIndicator = ({
  className,
  width = "w-[25px]",
  activeHashboard = 0,
  totalHashboards = 6,
}: HashboardIndicatorProps) => {
  return (
    <div className={clsx(width, className)}>
      <svg
        width="100%"
        height="100%"
        viewBox="0 0 25 18"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path
          d="M1.1665 6.4C1.1665 5.27164 1.16689 4.45545 1.21934 3.81352C1.27131 3.17744 1.37189 2.75662 1.54798 2.41103C1.88354 1.75247 2.41897 1.21703 3.07754 0.881477C3.42313 0.705391 3.84394 0.604807 4.48003 0.552836C5.12195 0.500389 5.93815 0.5 7.0665 0.5H18.2665C19.3949 0.5 20.2111 0.500389 20.853 0.552836C21.4891 0.604807 21.9099 0.705391 22.2555 0.881477C22.914 1.21703 23.4495 1.75247 23.785 2.41103C23.9611 2.75662 24.0617 3.17744 24.1137 3.81352C24.1661 4.45545 24.1665 5.27164 24.1665 6.4V11.6C24.1665 12.7284 24.1661 13.5446 24.1137 14.1865C24.0617 14.8226 23.9611 15.2434 23.785 15.589C23.4495 16.2475 22.914 16.783 22.2555 17.1185C21.9099 17.2946 21.4891 17.3952 20.853 17.4472C20.2111 17.4996 19.3949 17.5 18.2665 17.5H7.0665C5.93815 17.5 5.12195 17.4996 4.48003 17.4472C3.84394 17.3952 3.42313 17.2946 3.07754 17.1185C2.41897 16.783 1.88354 16.2475 1.54798 15.589C1.37189 15.2434 1.27131 14.8226 1.21934 14.1865C1.16689 13.5446 1.1665 12.7284 1.1665 11.6V6.4Z"
          stroke="currentColor"
          strokeOpacity="0.10"
        />

        {Array.from(Array(totalHashboards)).map((_, index) => (
          <rect
            key={index}
            x={4.6665 + (index % (totalHashboards / 2)) * totalHashboards}
            y={4 + Math.floor(index / 3) * 6}
            width="4"
            height="4"
            rx="2"
            fill="currentColor"
            fillOpacity={index == activeHashboard ? ".5" : "0.1"}
          />
        ))}
      </svg>
    </div>
  );
};

export default HashboardIndicator;
