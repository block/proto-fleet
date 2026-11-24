// Bounds Frigate enforces for the dry-cooler fan reversal duration, in whole minutes.
export const FAN_REVERSAL_MIN_MINUTES = 3;
export const FAN_REVERSAL_MAX_MINUTES = 10;

// Shown whenever a fan reversal request may or may not have started the PLC procedure.
export const FAN_REVERSAL_OUTCOME_UNKNOWN =
  "The controller did not confirm whether the fan reversal ran. A controller interlock can also stop it from starting without a refusal. Check the dry cooler state before trying again.";
