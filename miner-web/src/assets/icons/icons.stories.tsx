import { Fragment } from "react";

import {
  Alert,
  ArrowRight,
  Checkmark,
  ConcentricCircles,
  Dismiss,
  DismissCircle,
  DismissTiny,
  Info,
  InfoInverted,
  Logo,
  Minus,
  Pause,
  Plus,
  Power,
  Question,
  Stop,
  Success,
} from ".";

const icons = {
  Alert,
  ArrowRight,
  Checkmark,
  ConcentricCircles,
  Dismiss,
  DismissCircle,
  DismissTiny,
  Info,
  InfoInverted,
  Logo,
  Minus,
  Pause,
  Plus,
  Power,
  Question,
  Stop,
  Success,
};

interface IconProps {
  Icons: (keyof typeof icons)[];
}

const IconWrapper = ({ Icons }: IconProps) => {
  const loopCount = Math.ceil(Icons.length / 5);
  return [...Array(loopCount)].map((_, index) => {
    const start = index * 5;
    const end = start + 5;
    const slicedIcons = Icons.slice(start, end);
    return (
      <tr key={index}>
        {slicedIcons.map((icon: keyof typeof icons) => {
          const Icon = icons[icon];
          return (
            <Fragment key={icon}>
              <td className="p-4 border border-r-transparent border-border-primary/5">
                {icon}
              </td>
              <td className="border border-l-transparent border-border-primary/5 m-auto">
                <div className="flex justify-center">
                  <Icon />
                </div>
              </td>
            </Fragment>
          );
        })}
      </tr>
    );
  });
};

export const Icons = () => {
  return (
    <table className="w-full table-fixed">
      <tbody>
        <IconWrapper Icons={Object.keys(icons) as (keyof typeof icons)[]} />
      </tbody>
    </table>
  );
};

export default {
  title: "Style/Icons",
};
