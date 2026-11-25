import {
  dismissFanReversalOutcome,
  fanReversalMessage,
  fanReversalWaitingMessage,
  useFanReversalOutcome,
} from "../hooks/useFanReversalOutcome";
import { OUTCOME_TONES } from "../hooks/usePlantAlarmOutcome";
import Button from "@/shared/components/Button";

/**
 * The fan reversal's progress or result while the System controls modal is closed, since a run
 * lasts minutes and keeps going after the modal closes.
 */
export default function FanReversalOutcomeNotice() {
  const { generation, outcome } = useFanReversalOutcome();
  const result = fanReversalMessage(outcome);
  if (result) {
    return (
      <div className="flex items-center gap-2">
        <p role={result.tone === "neutral" ? "status" : "alert"} className={`text-200 ${OUTCOME_TONES[result.tone]}`}>
          {result.message}
        </p>
        <Button variant="textOnly" text="Dismiss" onClick={() => dismissFanReversalOutcome(generation)} />
      </div>
    );
  }
  if (outcome?.phase === "running") {
    return (
      <p role="status" className="text-200 text-text-primary-70">
        {fanReversalWaitingMessage(outcome.durationMinutes)}
      </p>
    );
  }
  return null;
}
