import { type ReactElement } from "react";
import { isEditableResponseProfile, type ResponseProfile } from "@/protoFleet/api/curtailmentTypes";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import Button, { sizes, variants } from "@/shared/components/Button";
import Card, { cardType } from "@/shared/components/Card";
import ProgressCircular from "@/shared/components/ProgressCircular";

function ResponseProfilesEmptyState(): ReactElement {
  return (
    <SettingsEmptyState
      size="section"
      title="No response profiles configured"
      description="Add a profile to reuse curtailment actions across automation rules."
    />
  );
}

function ResponseProfilesLoadingState(): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full items-center justify-center py-14">
      <ProgressCircular indeterminate dataTestId="curtailment-response-profiles-loading" />
    </div>
  );
}

type ResponseProfilesErrorStateProps = {
  message: string;
};

function ResponseProfilesErrorState({ message }: ResponseProfilesErrorStateProps): ReactElement {
  return <SettingsEmptyState size="section" title="Unable to load response profiles" description={message} />;
}

type ResponseProfileCardProps = {
  profile: ResponseProfile;
  onEdit: (profile: ResponseProfile) => void;
};

function ResponseProfileCard({ profile, onEdit }: ResponseProfileCardProps): ReactElement {
  return (
    <Card
      title={<span data-testid="response-profile-name">{profile.name}</span>}
      type={cardType.default}
      className="curtailment-response-profile-card bg-surface-elevated-base shadow-100"
      headerTone="neutral"
      headerClassName="items-start bg-surface-elevated-base px-6 pt-6 pb-0"
      titleClassName="truncate text-emphasis-300 leading-5 font-semibold text-text-primary"
      bodyClassName="px-6 pt-0 pb-1"
      testId="response-profile-card"
      headerAction={
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Edit"
          ariaLabel={isEditableResponseProfile(profile) ? "Edit" : "Editing this profile is unavailable"}
          className="!h-8 !px-3 !py-0"
          disabled={!isEditableResponseProfile(profile)}
          onClick={() => onEdit(profile)}
          testId={`response-profile-edit-${profile.id}`}
        />
      }
    >
      <div className="space-y-0 text-300 leading-[18px] text-text-primary-50">
        <p className="truncate">{profile.targetSummary}</p>
        <p className="truncate">{profile.scope}</p>
      </div>
    </Card>
  );
}

type ResponseProfileCardsProps = {
  profiles: ResponseProfile[];
  isLoading?: boolean;
  loadError?: string | null;
  onEdit: (profile: ResponseProfile) => void;
};

export function ResponseProfileCards({
  profiles,
  isLoading = false,
  loadError = null,
  onEdit,
}: ResponseProfileCardsProps): ReactElement {
  if (loadError) {
    return <ResponseProfilesErrorState message={loadError} />;
  }

  if (isLoading) {
    return <ResponseProfilesLoadingState />;
  }

  if (profiles.length === 0) {
    return <ResponseProfilesEmptyState />;
  }

  return (
    <div className="curtailment-response-profile-grid" data-testid="response-profile-card-grid">
      {profiles.map((profile) => (
        <ResponseProfileCard key={profile.id} profile={profile} onEdit={onEdit} />
      ))}
    </div>
  );
}
