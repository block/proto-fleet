import { IconProps } from "./types";

const Menu = ({ className }: IconProps) => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="20"
      height="20"
      viewBox="0 0 20 20"
      fill="none"
      className={className}
    >
      <path
        d="M16.1025 13.0049C16.6067 13.0562 17 13.4823 17 14C17 14.5177 16.6067 14.9438 16.1025 14.9951L16 15L4 15C3.44772 15 3 14.5523 3 14C3 13.4477 3.44772 13 4 13L16 13L16.1025 13.0049ZM16 5C16.5523 5 17 5.44772 17 6C17 6.55228 16.5523 7 16 7L4 7C3.44772 7 3 6.55228 3 6C3 5.44772 3.44772 5 4 5L16 5Z"
        fill="currentColor"
        fillOpacity="0.9"
      />
    </svg>
  );
};

export default Menu;
