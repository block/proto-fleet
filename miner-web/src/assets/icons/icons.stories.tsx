import { Fragment } from "react";

import {
  Alert,
  AlertCompact,
  ArrowRight,
  Caret,
  Checkmark,
  ConcentricCircles,
  Dismiss,
  Fan,
  Home,
  ImmersionCooling,
  Info,
  Logo,
  Mining,
  Pause,
  Power,
  Question,
  Settings,
  Success,
} from ".";

const icons = {
  Alert,
  AlertCompact,
  ArrowRight,
  Caret,
  Checkmark,
  ConcentricCircles,
  Dismiss,
  Fan,
  Home,
  ImmersionCooling,
  Info,
  Logo,
  Mining,
  Pause,
  Power,
  Question,
  Settings,
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
              <td className="border border-l-transparent border-border-primary/5">
                <Icon className="m-auto" />
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
