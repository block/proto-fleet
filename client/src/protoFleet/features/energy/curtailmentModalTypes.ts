import type {
  CurtailmentFormErrors,
  CurtailmentFormValues,
  CurtailmentPlanPreview,
  CurtailmentResponseProfileOption,
  CurtailmentSiteOption,
  CurtailmentStartModalMode,
  CurtailmentStartModalVariant,
  CurtailmentSubmitValues,
  ResponseProfileModalMode,
} from "@/protoFleet/api/curtailmentTypes";
import { type FacilityFanDeviceOption } from "@/protoFleet/features/energy/FacilityFanSelectionModal";

export interface CurtailmentStartModalProps {
  open: boolean;
  onDismiss: () => void;
  onSubmit: (values: CurtailmentSubmitValues) => void;
  /**
   * Called from edit mode when the operator requests a curtailment stop. The
   * parent owns confirmation and the stop-curtailment RPC.
   */
  onStopCurtailment?: () => void;
  onTestCurtailment?: (values: CurtailmentSubmitValues) => void;
  onDeleteResponseProfile?: () => void;
  mode?: CurtailmentStartModalMode;
  variant?: CurtailmentStartModalVariant;
  responseProfileMode?: ResponseProfileModalMode;
  initialValues?: Partial<CurtailmentFormValues>;
  responseProfiles?: CurtailmentResponseProfileOption[];
  siteOptions?: CurtailmentSiteOption[];
  infrastructureDevices?: FacilityFanDeviceOption[];
  isLoadingInfrastructureDevices?: boolean;
  infrastructureDevicesError?: string | null;
  onRetryInfrastructureDevices?: () => void;
  facilityFanSelectionDisabledReason?: string;
  defaultSiteScope?: CurtailmentSiteOption;
  siteScopeEnabled?: boolean;
  buildingScopeEnabled?: boolean;
  rackAndGroupScopeEnabled?: boolean;
  isSiteScopeLoading?: boolean;
  siteScopeDisabledReason?: string;
  errors?: CurtailmentFormErrors;
  preview?: CurtailmentPlanPreview;
  previewError?: string;
  actionError?: string | null;
  isSubmitting?: boolean;
  isTestingCurtailment?: boolean;
  isDeleting?: boolean;
}
