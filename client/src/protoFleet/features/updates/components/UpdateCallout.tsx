import { useUpdateStatus } from "@/protoFleet/features/updates/api/useUpdateStatus";
import { ArrowUp, Copy, Dismiss } from "@/shared/assets/icons";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { copyToClipboard } from "@/shared/utils/utility";

const DISMISSED_UPDATE_TAG_KEY = "dismissedUpdateTag";

// Pinned in the nav footer above the logout button. Only instance:update
// holders ever see it (the hook also gates the RPC), and each release tag can
// be dismissed independently: a newer eligible tag re-shows the callout.
const UpdateCallout = () => {
  const { status, hasUpdatePermission } = useUpdateStatus();
  const [dismissedTag, setDismissedTag] = useReactiveLocalStorage<string | undefined>(DISMISSED_UPDATE_TAG_KEY);

  const release = status?.latestEligible;
  if (!hasUpdatePermission || !status?.updateAvailable || !release || release.version === dismissedTag) {
    return null;
  }

  const handleCopy = () => {
    copyToClipboard(status.installCommand)
      .then(() => {
        pushToast({
          message: "Install command copied to clipboard",
          status: STATUSES.success,
        });
      })
      .catch(() => {
        pushToast({
          message: "Failed to copy install command",
          status: STATUSES.error,
        });
      });
  };

  return (
    <div data-testid="update-callout" className="mb-1 w-full">
      {/* Collapsed laptop nav: icon-only affordance. Hovering expands the nav
          (group/nav), which swaps this for the full callout below. */}
      <div
        data-testid="update-callout-collapsed"
        aria-hidden="true"
        className="hidden h-10 w-full items-center px-2.5 py-2 laptop:flex laptop:group-hover/nav:hidden desktop:hidden"
      >
        <div className="relative flex size-5 shrink-0 items-center justify-center">
          <ArrowUp className="text-text-primary-70" width="w-5" />
          <span className="absolute -top-0.5 -right-0.5 size-2 rounded-full bg-intent-info-fill" />
        </div>
      </div>

      {/* Full callout: expanded-on-hover laptop nav, always on desktop and on
          the full-width mobile/tablet floating menu. */}
      <div
        data-testid="update-callout-expanded"
        className="w-full rounded-lg bg-core-primary-5 p-2.5 laptop:hidden laptop:group-hover/nav:block desktop:block"
      >
        <div className="flex w-full items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="text-emphasis-300 whitespace-nowrap text-text-primary">Update available</div>
            <div className="truncate text-200 text-text-primary-50">{release.version}</div>
          </div>
          <Dismiss
            ariaLabel="Dismiss update notification"
            onClick={() => setDismissedTag(release.version)}
            width="w-3"
            className="text-text-primary-50 hover:text-text-primary"
          />
        </div>
        <a
          href={release.releaseNotesUrl}
          target="_blank"
          rel="noreferrer"
          className="mt-0.5 inline-block text-200 text-text-primary-70 underline underline-offset-2 hover:text-text-primary"
        >
          Release notes
        </a>
        <button
          type="button"
          onClick={handleCopy}
          className="mt-2 flex h-8 w-full items-center gap-2 rounded-lg bg-core-primary-10 px-2 text-200 whitespace-nowrap text-text-primary hover:cursor-pointer hover:bg-core-primary-20"
        >
          <Copy width="w-4" />
          Copy install command
        </button>
      </div>
    </div>
  );
};

export default UpdateCallout;
