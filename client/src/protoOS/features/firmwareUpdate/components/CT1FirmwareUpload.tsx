import { useRef, useState } from "react";
import clsx from "clsx";
import type { CT1Firmware, CT1FirmwareOutcome } from "@/protoOS/api/adapters/ct1Firmware";
import { useCT1Firmware } from "@/protoOS/api/hooks/useCT1Firmware";
import Button from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import FileSizeValue from "@/shared/components/FileSizeValue";

const outcomeText: Record<CT1FirmwareOutcome, string> = {
  completed: "Update finished",
  completedWithErrors: "Update finished with errors",
  canceled: "Update canceled",
  interrupted: "Update interrupted",
  failed: "Update failed",
};

// The board list gives up height before the dialog itself has to scroll, so the footer stays on screen in a short
// window. Each offset is the height of everything else in the dialog plus room for a message; the footer is taller
// once a third button makes it stack.
const boardListHeight = {
  row: "max-h-[clamp(6rem,calc(100dvh_-_29rem),20rem)]",
  stacked: "max-h-[clamp(4.5rem,calc(100dvh_-_37rem),20rem)]",
};

export default function CT1FirmwareUpload({ firmware, deviceId }: { firmware: CT1Firmware; deviceId: string }) {
  const state = useCT1Firmware(firmware, deviceId);
  const [open, setOpen] = useState(false);
  const [confirmingRelease, setConfirmingRelease] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const close = () => {
    if (!state.busy) setOpen(false);
  };
  const outcome = state.progress?.outcome;
  const unconfirmed = state.attempt?.cancel === "unconfirmed";
  // One line per distinct message: a failed cancel and the status poll often report the same unreachable controller.
  const alerts = [
    ...new Set([...(state.attempt ? (state.progress?.errors ?? []) : []), ...(state.error ? [state.error] : [])]),
  ].map((message) => (
    <p key={message} role="alert" className="text-text-critical">
      {message}
    </p>
  ));
  return (
    <div className="mt-6 flex justify-center">
      <Button variant="secondary" size="compact" onClick={() => setOpen(true)}>
        {state.attempt ? "Hashboard update status" : "Upload hashboard firmware"}
      </Button>
      {open && confirmingRelease && state.attempt ? (
        <Dialog
          title="Stop tracking this update?"
          subtitle="This browser will no longer show or cancel it. An update still running on a controller that did not confirm the cancel continues."
          subtitleSize="text-300"
          manageFocus
          onDismiss={() => setConfirmingRelease(false)}
          buttons={[
            { text: "Keep tracking", variant: "secondary", onClick: () => setConfirmingRelease(false) },
            {
              text: "Stop tracking",
              variant: "primary",
              onClick: () => {
                state.clear();
                setConfirmingRelease(false);
              },
            },
          ]}
        />
      ) : open ? (
        <Dialog
          title={state.attempt ? "Hashboard update status" : "Upload hashboard firmware"}
          manageFocus
          onDismiss={close}
          buttons={[
            { text: "Close", variant: "secondary", disabled: state.busy, onClick: close },
            ...(state.attempt
              ? outcome
                ? [
                    {
                      text: "Done",
                      variant: "primary" as const,
                      onClick: () => {
                        state.clear();
                        setOpen(false);
                      },
                    },
                  ]
                : [
                    ...(unconfirmed
                      ? [
                          {
                            text: "Stop tracking",
                            variant: "secondary" as const,
                            disabled: state.busy,
                            onClick: () => setConfirmingRelease(true),
                          },
                        ]
                      : []),
                    {
                      text: state.attempt.cancel ? "Retry cancel" : "Cancel update",
                      variant: "primary" as const,
                      disabled: state.busy,
                      onClick: state.cancel,
                    },
                  ]
              : [
                  {
                    text: state.busy ? "Cancel upload" : "Upload and install",
                    variant: "primary" as const,
                    disabled: !state.busy && !state.image,
                    onClick: state.busy ? state.cancel : state.install,
                  },
                ]),
          ]}
        >
          <div className="space-y-4 text-300">
            {!state.attempt ? (
              <>
                <p>
                  Choose an F1 .bin image to update every occupied hashboard. Mining pauses during installation. Keep
                  the container powered on.
                </p>
                <Button variant="secondary" disabled={state.busy} onClick={() => input.current?.click()}>
                  {state.image ? "Change file" : "Choose file"}
                </Button>
                <input
                  ref={input}
                  type="file"
                  accept=".bin"
                  aria-label="Hashboard firmware file"
                  className="hidden"
                  disabled={state.busy}
                  onChange={(event) => {
                    const file = event.target.files?.[0];
                    event.target.value = "";
                    if (file) void state.choose(file);
                  }}
                />
                {state.image ? (
                  <div className="break-all">
                    <p>
                      {state.image.file.name} · <FileSizeValue value={state.image.file.size} />
                    </p>
                    <p>
                      Version {state.image.version} · {state.image.build} · {state.boards.length} hashboards
                    </p>
                    <p className="text-text-primary-70">Installs even if this version is already present.</p>
                  </div>
                ) : null}
                {state.busy ? (
                  <p role="status">
                    {state.image ? `Uploading firmware: ${state.uploadPercent}%` : "Reading firmware and board map…"}{" "}
                    Keep this page open.
                  </p>
                ) : null}
                {alerts}
              </>
            ) : (
              <>
                <p>
                  Version {state.attempt.version} · {state.attempt.build} · {state.attempt.boards.length} hashboards
                </p>
                <p role="status">
                  {state.busy
                    ? "Confirming update…"
                    : outcome
                      ? outcomeText[outcome]
                      : unconfirmed
                        ? "The cancel was not confirmed by every controller."
                        : state.attempt.cancel
                          ? "Cancel requested. Waiting for hashboards to stop…"
                          : "Checking update status…"}
                </p>
                {alerts}
                <ul
                  className={clsx(
                    "space-y-3 overflow-y-auto",
                    unconfirmed && !outcome ? boardListHeight.stacked : boardListHeight.row,
                  )}
                  aria-label="Hashboard update progress"
                >
                  {state.progress?.boards.map((board) => (
                    <li key={board.mac}>
                      <p>{board.label}</p>
                      <p className="text-text-primary-70">
                        {board.status}
                        {board.percent !== undefined ? ` · ${Math.round(board.percent)}%` : ""}
                        {board.message ? ` · ${board.message}` : ""}
                      </p>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </div>
        </Dialog>
      ) : null}
    </div>
  );
}
