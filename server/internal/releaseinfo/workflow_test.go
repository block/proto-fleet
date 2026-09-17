package releaseinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type releaseWorkflow struct {
	On struct {
		WorkflowCall struct {
			Inputs map[string]struct {
				Required bool `yaml:"required"`
			} `yaml:"inputs"`
		} `yaml:"workflow_call"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Uses  string            `yaml:"uses"`
		With  map[string]string `yaml:"with"`
		Env   map[string]string `yaml:"env"`
		Steps []struct {
			Run  string            `yaml:"run"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readReleaseWorkflow(t *testing.T, name string) releaseWorkflow {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("../../..", ".github/workflows", name))
	require.NoError(t, err)
	var workflow releaseWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))
	return workflow
}

func TestPublishingWorkflowsCarryRepository(t *testing.T) {
	t.Parallel()
	build := readReleaseWorkflow(t, "proto-fleet-artifact-build.yml")
	require.True(t, build.On.WorkflowCall.Inputs["release_repository"].Required)
	for _, name := range []string{"build-proto-fleet-server", "package-fleet-node", "build-proto-fleet"} {
		require.Equal(t, "${{ needs.metadata.outputs.release_repository }}", build.Jobs[name].Env["RELEASE_REPOSITORY"])
	}
	for _, name := range []string{"release.yml", "nightly-builds.yml"} {
		workflow := readReleaseWorkflow(t, name)
		foundBuild, foundInstallers := false, false
		for _, job := range workflow.Jobs {
			if job.Uses == "./.github/workflows/proto-fleet-artifact-build.yml" {
				foundBuild = true
				require.Equal(t, "${{ github.repository }}", job.With["release_repository"])
			}
			for _, step := range job.Steps {
				if step.With["name"] == "proto-fleet-release-installers" {
					foundInstallers = true
				}
				require.NotContains(t, step.Run, "./deployment-files/install.sh")
				require.NotContains(t, step.Run, "./deployment-files/fleetnode/install-fleet-node.sh")
			}
		}
		require.True(t, foundBuild, name)
		require.True(t, foundInstallers, name)
	}
}

func TestBuildValidatesPublishingRepositoryBeforeEmittingMetadata(t *testing.T) {
	t.Parallel()
	workflow := readReleaseWorkflow(t, "proto-fleet-artifact-build.yml")
	script := workflow.Jobs["metadata"].Steps[0].Run
	for _, repository := range []string{"example-owner/fleet-fork", "block/proto-fleet", "owner/repo;id", "owner/repo/extra"} {
		t.Run(repository, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			output := filepath.Join(dir, "output")
			cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", script)
			cmd.Env = append(os.Environ(),
				"RELEASE_REPOSITORY="+repository, "GITHUB_REPOSITORY=example-owner/fleet-fork",
				"INPUT_VERSION=v1.2.3", "INPUT_CHANNEL=release", "INPUT_BUILD_DATE=", "INPUT_IS_PRERELEASE=false",
				"GITHUB_OUTPUT="+output, "GITHUB_STEP_SUMMARY="+filepath.Join(dir, "summary"), "GITHUB_SHA=fixture",
			)
			result, err := cmd.CombinedOutput()
			if repository != "example-owner/fleet-fork" {
				require.Error(t, err, string(result))
				require.NoFileExists(t, output)
				return
			}
			require.NoError(t, err, string(result))
			metadata, err := os.ReadFile(output)
			require.NoError(t, err)
			require.Contains(t, string(metadata), "release_repository="+repository+"\n")
		})
	}
}
