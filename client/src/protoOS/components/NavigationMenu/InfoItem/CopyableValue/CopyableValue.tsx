import { Copy } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

export interface CopyableValueProps {
  // Field name for the button's accessible name and the toast message.
  label: string;
  value: string;
}

// The copy button stays hidden until the pointer is over this value or over an
// ancestor that also sets `group/copyable`, such as the full info row. It stays
// in the tab order, and touch devices have no hover, so keyboard focus and
// coarse pointers show it too.
const CopyableValue = ({ label, value }: CopyableValueProps) => {
  return (
    <div className="group/copyable flex items-center gap-2.5">
      <span className="min-w-0 wrap-anywhere">{value}</span>
      <span className="shrink-0 opacity-0 transition-opacity group-hover/copyable:opacity-100 focus-within:opacity-100 pointer-coarse:opacity-100">
        <Button
          variant={variants.textOnly}
          size={sizes.textOnly}
          textColor="text-text-primary-50"
          textOnlyUnderlineOnHover={false}
          prefixIcon={<Copy width={iconSizes.xSmall} />}
          ariaLabel={`Copy ${label} ${value}`}
          className="-m-1 p-1"
          onClick={() => {
            void copyToClipboard(value)
              .then(() => pushToast({ message: `${label} copied to clipboard`, status: STATUSES.success }))
              .catch(() => pushToast({ message: `Failed to copy ${label}`, status: STATUSES.error }));
          }}
        />
      </span>
    </div>
  );
};

export default CopyableValue;
