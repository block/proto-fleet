// Inline RPC-failure banner shared by the add and detail modals,
// following the CurtailmentStartModal actionError pattern.
const ActionErrorBanner = ({ message }: { message: string }) => (
  <div
    className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical"
    data-testid="infra-device-action-error"
  >
    {message}
  </div>
);

export default ActionErrorBanner;
