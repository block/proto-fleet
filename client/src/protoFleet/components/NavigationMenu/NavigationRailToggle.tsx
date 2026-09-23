import type { NavigationRail } from "./useNavigationRail";

export default function NavigationRailToggle({ rail }: { rail: NavigationRail }) {
  if (!rail.isCollapsible) return null;
  const label = rail.isExpanded ? "Collapse navigation" : "Expand navigation";
  return (
    <button
      ref={rail.toggleRef}
      type="button"
      aria-label={label}
      title={label}
      aria-expanded={rail.isExpanded}
      aria-controls={rail.navigationId}
      onClick={rail.toggle}
      className="-ml-2 flex h-[44px] shrink-0 cursor-pointer items-center justify-center rounded-lg px-[4px] text-text-primary-50 hover:bg-core-primary-5 hover:text-text-primary focus-visible:text-text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-text-primary aria-expanded:text-text-primary"
    >
      <svg aria-hidden="true" className="size-6" viewBox="0 0 24 24" fill="none">
        {/* Market panel-left: squareup/market/common/icons/assets/svg/panel-left.svg */}
        <path d="M9 17H6V7H9V17Z" fill="currentColor" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M18 3C20.2091 3 22 4.79086 22 7V17C22 19.14 20.3194 20.8879 18.2061 20.9951L18 21H6L5.79395 20.9951C3.7488 20.8913 2.10865 19.2512 2.00488 17.2061L2 17V7C2 4.79086 3.79086 3 6 3H18ZM6 5C4.89543 5 4 5.89543 4 7V17C4 18.1046 4.89543 19 6 19H18C19.1046 19 20 18.1046 20 17V7C20 5.89543 19.1046 5 18 5H6Z"
          fill="currentColor"
        />
      </svg>
    </button>
  );
}
