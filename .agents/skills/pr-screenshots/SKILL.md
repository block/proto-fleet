---
name: pr-screenshots
description: Capture UI screenshots (desktop, mobile, light, and dark) and add them to a PR description as GitHub attachments with `gh pr edit --attach`. Never commit the images. Use for PRs that change ProtoFleet or ProtoOS UI.
argument-hint: "(optional: PR number/URL; defaults to the current branch PR)"
---

Add screenshots of the UI changes to the description of the target PR.
Screenshots help reviewers, but they must not go into the repository. Upload
them as GitHub attachments only.

Save the optional PR number or URL supplied with the skill invocation as
`pr_ref`.

## Rules

- Do not commit image files to any branch. This includes asset branches such
  as `pr-assets/*`.
- Keep all image files outside the worktree.
- Capture only fake or seed data. Do not capture real customer data, secrets,
  or tokens.
- Do not remove or rewrite text in the PR body that is not in the
  `## Screenshots` section.

## Steps

1. **Resolve the target PR.**
   - If `pr_ref` is set, it must match `^[0-9]+$` or
     `^https://github\.com/[A-Za-z0-9-]+/[A-Za-z0-9._-]+/pull/[0-9]+$`. If it
     does not match, stop and ask for a clean identifier. Always double-quote
     it in shell commands.
   - Run `gh pr view "$pr_ref" --json number,url,headRefName,headRefOid`. If
     `pr_ref` is empty, omit it to use the current branch PR. If no PR exists,
     stop and tell the user to open the PR first.
   - Parse `owner/repo` from the returned `url`. Pass
     `-R <owner>/<repo> <number>` to every later `gh pr` command.
   - Steps 3 to 5 run the code in the local worktree. Make sure that the
     worktree contains the PR head:

     ```bash
     git merge-base --is-ancestor "<headRefOid>" HEAD
     ```

     If the command fails for any reason, stop. Tell the user to check out
     the PR branch (`<headRefName>`) first. Otherwise the screenshots can
     show code that is not in the PR.

2. **Check the `gh` version.** The `--attach` flag on `gh pr edit` needs gh
   2.99 or later. The Hermit package in `bin/` is older and does not have the
   flag. Find a
   `gh` that has it, for example `/opt/homebrew/bin/gh`:

   ```bash
   for candidate in $(which -a gh); do
     "$candidate" pr edit --help 2>/dev/null | grep -q -- '--attach' && { gh_bin="$candidate"; break; }
   done
   ```

   Use `"$gh_bin"` for the `gh pr edit` command in step 7. If no candidate
   has the flag, stop and tell the user to install gh 2.99 or later.

3. **Select the states to capture.** Read
   `gh pr diff <number> -R <owner>/<repo> --name-only` and the changed
   components. List each visible state that the PR adds or changes.
   Examples: default, empty, loading, error, and the specific state that the
   PR fixes. Include a "before" image only when the change is hard to see
   without it.

   Then select the viewports and themes for each state:

   | Variant | Size or setting | When to capture |
   | --- | --- | --- |
   | Desktop, light | 1600×900 | Always |
   | Mobile | 393×852 | The PR changes a ProtoOS page or component, or changes responsive styles in ProtoFleet |
   | Dark | `dark` color scheme | The PR adds a component, or changes colors, theme tokens, status colors, charts, borders, or backgrounds |

   The sizes are the same as the `desktop` and `mobile` projects in
   `client/e2eTests/*/playwright.config.ts`. ProtoFleet has no mobile E2E
   project, so do not capture mobile for ProtoFleet only to fill the matrix.
   When both mobile and dark apply, capture desktop dark and mobile light.
   Capture mobile dark only when the change is specific to mobile dark mode.

4. **Start a UI source.** Use the source that shows the change with the least
   setup:
   - ProtoOS container pages: `just dev-ct1` runs the client against the fake
     CT1 simulator. Set `CT1_TANKS`, `CT1_MODULES_PER_TANK`, or
     `CT1_DEMO_MODULE_STATES` to get the necessary states.
   - Isolated components or edge-case states: `npm run storybook` in
     `client/` (port 6006). Open a single story at
     `http://localhost:6006/iframe.html?id=<story-id>&viewMode=story`.
   - ProtoFleet pages: `just dev`.

   Run long-lived servers in the background, and stop them when you finish.

5. **Capture the images.** Make a temporary directory outside the worktree,
   for example `shots_dir="$(mktemp -d)"`. Save one PNG for each state and
   variant. Name each file `<state>-<viewport>-<theme>.png`, for example
   `system-view-desktop-light.png` or `system-view-mobile-light.png`.

   Use a browser tool that can save a PNG. For example, run these commands
   from `client/`, which has Playwright:

   ```bash
   npx playwright screenshot --viewport-size=1600,900 --color-scheme=light "<url>" "$shots_dir/system-view-desktop-light.png"
   npx playwright screenshot --viewport-size=1600,900 --color-scheme=dark "<url>" "$shots_dir/system-view-desktop-dark.png"
   npx playwright screenshot --viewport-size=393,852 --color-scheme=light "<url>" "$shots_dir/system-view-mobile-light.png"
   ```

   Set the theme as follows:
   - App pages (`just dev-ct1`, `just dev`): the default theme is `system`,
     so the app follows `--color-scheme`. Use a new browser context with no
     saved theme preference. If the app shows the wrong theme, select the
     theme in the app settings before you capture.
   - Storybook: the stories ignore `--color-scheme`. Add
     `&globals=theme:dark` to the story URL for dark mode.

   Open each image and make sure that it shows the correct state, viewport,
   and theme. Make sure that it shows no loading spinners or error overlays.
   If the page loads data after the first render, add
   `--wait-for-selector "<selector>"`.

6. **Write the new body.** Fetch the current body immediately before you
   edit, so that you do not overwrite recent changes:

   ```bash
   gh pr view <number> -R <owner>/<repo> --json body -q .body > "$shots_dir/body.md"
   ```

   Add a `## Screenshots` section to `$shots_dir/body.md`, or replace the
   existing one. Put one short caption for each state. If a state has one
   image, put the image under the caption:

   ```markdown
   ## Screenshots

   System view with one faulted tank:

   ![System view with one faulted tank](./system-view-desktop-light.png)
   ```

   If a state has more than one variant, put the variants side by side in a
   table. This keeps the section short:

   ```markdown
   System view with one faulted tank:

   | Desktop, light | Desktop, dark | Mobile, light |
   | --- | --- | --- |
   | ![System view, desktop, light](./system-view-desktop-light.png) | ![System view, desktop, dark](./system-view-desktop-dark.png) | ![System view, mobile, light](./system-view-mobile-light.png) |
   ```

   Make the alt text of each image include its viewport and theme.

   Keep `body.md` in the same directory as the images. Step 7 runs from that
   directory, so every gh version that has `--attach` resolves the `./<file>`
   references to the correct files.

7. **Upload the images and update the body.** Run the command from
   `$shots_dir`. Give one `--attach` flag for each image, with a `./` path and
   its alt text after `#`:

   ```bash
   (cd "$shots_dir" && "$gh_bin" pr edit <number> -R <owner>/<repo> \
     --body-file body.md \
     --attach "./system-view-desktop-light.png#System view, desktop, light" \
     --attach "./system-view-desktop-dark.png#System view, desktop, dark" \
     --attach "./system-view-mobile-light.png#System view, mobile, light")
   ```

   gh 2.103 and later resolve `./<file>` from the directory of the body file.
   Earlier versions resolve it from the working directory. The two
   directories are the same here, so the references match on all versions.

   A reference that is already in the body keeps the alt text from the body.

   `gh` uploads each file and replaces the matching `./<file>` reference with
   the uploaded URL.

   If the command exits with a non-zero status, some uploads failed. gh still
   updates the PR body with the images that uploaded. The local `body.md`
   still has `./<file>` references for those images, so do not send it
   again. To retry:
   1. Read the output and fix the cause.
   2. Fetch the current PR body into `$shots_dir/body.md` again, as in
      step 6.
   3. Run the step 7 command again with only the files that failed.

8. **Verify.**
   - Run `gh pr view <number> -R <owner>/<repo> --json body -q .body`. Make
     sure that each image reference points to an uploaded URL and that no
     `./<file>` path remains.
   - Run `git status --short` and `gh pr diff <number> -R <owner>/<repo>
     --name-only`. Make sure that no image file appears in either output.
   - Delete `$shots_dir` after the PR body is correct.

9. **Report.** Give the PR URL and the list of states that you captured.
   For each state, give the viewports, the themes, and the UI source. Tell
   the user about each state that you could not capture, and give the
   reason.
