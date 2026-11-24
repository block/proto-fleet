package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

const preparedImagesFilename = ".prepared-images.json"

// The updater syncs this proof and binds it into the prepared release's content
// digest. Image archives are not needed again after these identities are saved.
func recordPreparedApplicationImages(ctx context.Context, root string, profile fleetApplicationProfile, composeOutput func(context.Context, []string) ([]byte, error), run func(context.Context, string, ...string) ([]byte, error)) error {
	images, err := applicationImageIDs(ctx, root, profile, composeOutput, run)
	if err != nil {
		return err
	}
	data, err := json.Marshal(images)
	if err != nil {
		return fmt.Errorf("encode prepared application image identities: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, preparedImagesFilename), data, 0o600); err != nil {
		return fmt.Errorf("record prepared application image identities: %w", err)
	}
	return nil
}

// CheckApplicationImages confirms every application image still resolves to the
// identity recorded during preparation. It never pulls or loads images.
func CheckApplicationImages(ctx context.Context, root string) error {
	profile, err := loadUpdateCompatibleProfile(installedFleetEnvironment)
	if err != nil {
		return err
	}
	return checkApplicationImages(ctx, root, profile, readComposeOutput, defaultInstallDependencies().run)
}

func checkApplicationImages(ctx context.Context, root string, profile fleetApplicationProfile, composeOutput func(context.Context, []string) ([]byte, error), run func(context.Context, string, ...string) ([]byte, error)) error {
	data, err := os.ReadFile(filepath.Join(root, preparedImagesFilename))
	if err != nil {
		return fmt.Errorf("read prepared application image identities; prepare the release again: %w", err)
	}
	var expected map[string]string
	if err := json.Unmarshal(data, &expected); err != nil {
		return fmt.Errorf("decode prepared application image identities; prepare the release again: %w", err)
	}
	images, err := applicationImageIDs(ctx, root, profile, composeOutput, run)
	if err != nil {
		return err
	}
	if !maps.Equal(images, expected) {
		return errors.New("prepared application image identities changed; prepare the release again")
	}
	return nil
}

func applicationImageIDs(ctx context.Context, root string, profile fleetApplicationProfile, composeOutput func(context.Context, []string) ([]byte, error), run func(context.Context, string, ...string) ([]byte, error)) (map[string]string, error) {
	output, err := composeOutput(ctx, fleetApplicationComposeArgsAtProfile(root, installedFleetEnvironment, profile, "config", "--images"))
	if err != nil {
		return nil, err
	}
	images := strings.Fields(string(output))
	if len(images) == 0 {
		return nil, errors.New("prepared application has no resolved images")
	}
	output, err = run(ctx, "docker", append([]string{"--host", localDockerHost, "image", "inspect", "--format", "{{.Id}}"}, images...)...)
	if err != nil {
		return nil, fmt.Errorf("prepared application images are no longer available; prepare the release again: %s", commandError(output, err))
	}
	ids := strings.Fields(string(output))
	if len(ids) != len(images) {
		return nil, errors.New("Docker returned incomplete application image identities; prepare the release again")
	}
	identities := make(map[string]string, len(images))
	for i, image := range images {
		identities[image] = ids[i]
	}
	return identities, nil
}
