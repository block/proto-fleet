import {
  OUTCOME_TONES,
  outcomeMessage,
  setPlantAlarmOutcome,
  useNow,
  usePlantAlarmReadback,
} from "../hooks/usePlantAlarmOutcome";
import Button from "@/shared/components/Button";

/**
 * The last alarm command's outcome, for places that show it while the Container alarms section
 * is closed: beside the System controls button, and when the controls cannot be loaded.
 */
export default function PlantAlarmOutcomeNotice() {
  const nowMs = useNow();
  const { generation, outcome, readback } = usePlantAlarmReadback(nowMs);
  const result = outcomeMessage(outcome, readback);
  if (result) {
    return (
      <div className="flex items-center gap-2">
        <p role={result.tone === "neutral" ? "status" : "alert"} className={`text-200 ${OUTCOME_TONES[result.tone]}`}>
          {result.message}
        </p>
        <Button variant="textOnly" text="Dismiss" onClick={() => setPlantAlarmOutcome(generation, undefined)} />
      </div>
    );
  }
  if (outcome?.phase === "sending" || outcome?.phase === "sent") {
    return (
      <p role="status" className="text-200 text-text-primary-70">
        {outcome.phase === "sending"
          ? "Sending alarm command…"
          : "Alarm command sent; waiting for the controller to report…"}
      </p>
    );
  }
  return null;
}
