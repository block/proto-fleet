import { useRef, useState } from "react";
import type { CT1Firmware } from "@/protoOS/api/adapters/ct1Firmware";
import { useCT1Firmware } from "@/protoOS/api/hooks/useCT1Firmware";
import Button from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import FileSizeValue from "@/shared/components/FileSizeValue";

export default function CT1FirmwareUpload({ firmware, deviceId }: { firmware: CT1Firmware; deviceId: string }) {
  const state = useCT1Firmware(firmware, deviceId);
  const [open, setOpen] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const close = () => {
    if (!state.busy) setOpen(false);
  };
  return (
    <div className="mt-6 flex justify-center">
      <Button variant="secondary" size="compact" onClick={() => setOpen(true)}>
        {state.attempt ? "Hashboard update status" : "Upload hashboard firmware"}
      </Button>
      {open ? (
        <Dialog
          title={state.attempt ? "Hashboard update status" : "Upload hashboard firmware"}
          manageFocus
          onDismiss={close}
          buttons={[
            { text: "Close", variant: "secondary", disabled: state.busy, onClick: close },
            ...(state.attempt
              ? [
                  state.progress?.complete
                    ? {
                        text: "Done",
                        variant: "primary" as const,
                        onClick: () => {
                          state.clear();
                          setOpen(false);
                        },
                      }
                    : {
                        text: state.attempt.cancelRequested ? "Retry cancel" : "Cancel update",
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
              </>
            ) : (
              <>
                <p>
                  Version {state.attempt.version} · {state.attempt.build} · {state.attempt.boards.length} hashboards
                </p>
                <p role="status">
                  {state.busy
                    ? "Confirming update…"
                    : state.progress?.complete
                      ? "Update finished"
                      : state.attempt.cancelRequested
                        ? "Cancel requested. Waiting for hashboards to stop…"
                        : "Checking update status…"}
                </p>
                {state.progress?.errors.map((error) => (
                  <p key={error} role="alert" className="text-text-critical">
                    {error}
                  </p>
                ))}
                <ul className="max-h-80 space-y-3 overflow-y-auto" aria-label="Hashboard update progress">
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
            {state.error ? (
              <p role="alert" className="text-text-critical">
                {state.error}
              </p>
            ) : null}
          </div>
        </Dialog>
      ) : null}
    </div>
  );
}
