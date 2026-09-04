import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

/**
 * Magnifying-glass glyph from Square Web's MarketMagnifyingGlassIcon, adapted
 * to Proto Fleet's existing currentColor icon wrapper.
 */
const Search = ({ className, width = iconSizes.medium }: IconProps) => (
  <div className={clsx(width, className)}>
    <svg
      width="100%"
      height="100%"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      preserveAspectRatio="xMidYMid meet"
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M5.11531 5.11531C7.93575 2.29488 12.5087 2.29491 15.3292 5.11531C17.9096 7.69576 18.1267 11.7413 15.9854 14.5714L21.2071 19.793L19.793 21.2071L14.5714 15.9854C11.7413 18.1267 7.69576 17.9096 5.11531 15.3292C2.29491 12.5087 2.29488 7.93575 5.11531 5.11531ZM13.9151 6.52938C11.8757 4.49002 8.56876 4.48999 6.52938 6.52938C4.48999 8.56876 4.49002 11.8757 6.52938 13.9151C8.56878 15.9545 11.8757 15.9545 13.9151 13.9151C15.9545 11.8757 15.9545 8.56878 13.9151 6.52938Z"
        fill="currentColor"
      />
    </svg>
  </div>
);

export default Search;
