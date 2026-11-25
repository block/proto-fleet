import { type SetStateAction, useCallback, useReducer } from "react";
import type { ForceInclusionFields, PendingCurtailmentConfirmation } from "./curtailmentConfirmation";
import { type CurtailmentTargetPath, getInitialCurtailmentTargetPath } from "./curtailmentFormScope";
import { getInitialValues } from "./curtailmentFormValidation";
import type {
  CurtailmentFormValues,
  CurtailmentSiteOption,
  CurtailmentStartModalVariant,
} from "@/protoFleet/api/curtailmentTypes";
interface FormState {
  initialFormValues: CurtailmentFormValues;
  values: CurtailmentFormValues;
  targetPath: CurtailmentTargetPath;
  showForceInclusionConfirmation: boolean;
  pendingForceInclusionValues: Partial<ForceInclusionFields> | null;
  confirmedForceInclusionKey: string;
  submitAfterForceInclusionConfirmation: PendingCurtailmentConfirmation["action"] | null;
  pendingCurtailmentConfirmation: PendingCurtailmentConfirmation | null;
  showMinerSelectionModal: boolean;
  showBuildingSelectionModal: boolean;
  showRackSelectionModal: boolean;
  showGroupSelectionModal: boolean;
  showSiteScopeModal: boolean;
  showFacilityFanSelectionModal: boolean;
  draftSelectedSiteIds: string[];
  editedFields: ReadonlySet<keyof CurtailmentFormValues>;
}
type FormAction = { [K in keyof FormState]: { field: K; update: SetStateAction<FormState[K]> } }[keyof FormState];
export function curtailmentFormReducer(state: FormState, action: FormAction): FormState {
  const current = state[action.field];
  const next =
    typeof action.update === "function"
      ? (action.update as (value: typeof current) => typeof current)(current)
      : action.update;
  return Object.is(current, next) ? state : { ...state, [action.field]: next };
}
export function useCurtailmentForm(
  initialValues: Partial<CurtailmentFormValues> | undefined,
  variant: CurtailmentStartModalVariant,
  defaultSiteScope?: CurtailmentSiteOption,
) {
  const [state, dispatch] = useReducer(curtailmentFormReducer, undefined, () => {
    const initialFormValues = getInitialValues(initialValues, variant, defaultSiteScope);
    return {
      initialFormValues,
      values: initialFormValues,
      targetPath: getInitialCurtailmentTargetPath(initialFormValues, initialValues),
      showForceInclusionConfirmation: false,
      pendingForceInclusionValues: null,
      confirmedForceInclusionKey: "",
      submitAfterForceInclusionConfirmation: null,
      pendingCurtailmentConfirmation: null,
      showMinerSelectionModal: false,
      showBuildingSelectionModal: false,
      showRackSelectionModal: false,
      showGroupSelectionModal: false,
      showSiteScopeModal: false,
      showFacilityFanSelectionModal: false,
      draftSelectedSiteIds: [],
      editedFields: new Set(),
    } as FormState;
  });
  const set = useCallback(
    <K extends keyof FormState>(field: K, update: SetStateAction<FormState[K]>) =>
      dispatch({ field, update } as FormAction),
    [],
  );
  return {
    ...state,
    setValues: (update: SetStateAction<FormState["values"]>) => set("values", update),
    setTargetPath: (update: SetStateAction<FormState["targetPath"]>) => set("targetPath", update),
    setShowForceInclusionConfirmation: (update: SetStateAction<FormState["showForceInclusionConfirmation"]>) =>
      set("showForceInclusionConfirmation", update),
    setPendingForceInclusionValues: (update: SetStateAction<FormState["pendingForceInclusionValues"]>) =>
      set("pendingForceInclusionValues", update),
    setConfirmedForceInclusionKey: (update: SetStateAction<FormState["confirmedForceInclusionKey"]>) =>
      set("confirmedForceInclusionKey", update),
    setSubmitAfterForceInclusionConfirmation: (
      update: SetStateAction<FormState["submitAfterForceInclusionConfirmation"]>,
    ) => set("submitAfterForceInclusionConfirmation", update),
    setPendingCurtailmentConfirmation: (update: SetStateAction<FormState["pendingCurtailmentConfirmation"]>) =>
      set("pendingCurtailmentConfirmation", update),
    setShowMinerSelectionModal: (update: SetStateAction<FormState["showMinerSelectionModal"]>) =>
      set("showMinerSelectionModal", update),
    setShowBuildingSelectionModal: (update: SetStateAction<FormState["showBuildingSelectionModal"]>) =>
      set("showBuildingSelectionModal", update),
    setShowRackSelectionModal: (update: SetStateAction<FormState["showRackSelectionModal"]>) =>
      set("showRackSelectionModal", update),
    setShowGroupSelectionModal: (update: SetStateAction<FormState["showGroupSelectionModal"]>) =>
      set("showGroupSelectionModal", update),
    setShowSiteScopeModal: (update: SetStateAction<FormState["showSiteScopeModal"]>) =>
      set("showSiteScopeModal", update),
    setShowFacilityFanSelectionModal: (update: SetStateAction<FormState["showFacilityFanSelectionModal"]>) =>
      set("showFacilityFanSelectionModal", update),
    setDraftSelectedSiteIds: (update: SetStateAction<FormState["draftSelectedSiteIds"]>) =>
      set("draftSelectedSiteIds", update),
    setEditedFields: (update: SetStateAction<FormState["editedFields"]>) => set("editedFields", update),
  };
}
