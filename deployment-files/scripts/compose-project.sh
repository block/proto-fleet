#!/bin/bash

# Shared Compose project-name parsing for the deployment runner and recovery
# commands. Callers set PROJECT_ROOT and ENV_FILE before resolving the name.

parse_compose_env_value() {
    local value="$1"
    local double_quoted='^"([^"\\]*)"[[:space:]]*(#.*)?$'
    local single_quoted="^'([^']*)'[[:space:]]*(#.*)?$"

    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    case "$value" in
        \"*)
            [[ "$value" =~ $double_quoted ]] || return 2
            value="${BASH_REMATCH[1]}"
            [[ "$value" != *'$'* && "$value" != *\\* ]] || return 2
            ;;
        \'*)
            [[ "$value" =~ $single_quoted ]] || return 2
            value="${BASH_REMATCH[1]}"
            ;;
        *)
            value="${value%%[[:space:]]#*}"
            value="${value%"${value##*[![:space:]]}"}"
            [[ "$value" != *'$'* ]] || return 2
            ;;
    esac
    printf '%s' "$value"
}

# Returns 1 when absent and 2 when the value uses unsupported Compose dotenv
# syntax that cannot be interpreted without risking divergence from Compose.
compose_env_last_value() {
    local key="$1" line normalized parsed found=false
    local assignment_re="^${key}[[:space:]]*[:=](.*)$"
    local malformed_re="^${key}([[:space:]]|$)"

    [ -e "$ENV_FILE" ] || return 1
    [ -f "$ENV_FILE" ] && [ -r "$ENV_FILE" ] || return 2
    while IFS= read -r line || [ -n "$line" ]; do
        normalized="${line#"${line%%[![:space:]]*}"}"
        case "$normalized" in
            export[[:space:]]*)
                normalized="${normalized#export}"
                normalized="${normalized#"${normalized%%[![:space:]]*}"}"
                ;;
        esac
        if [[ "$normalized" =~ $assignment_re ]]; then
            parsed=$(parse_compose_env_value "${BASH_REMATCH[1]}") || return 2
            found=true
        elif [[ "$normalized" =~ $malformed_re ]]; then
            return 2
        fi
    done < "$ENV_FILE"

    [ "$found" = "true" ] || return 1
    printf '%s' "$parsed"
}

resolve_compose_project_name() {
    local project_name persisted_status

    if [ -n "${COMPOSE_PROJECT_NAME:-}" ]; then
        project_name="$COMPOSE_PROJECT_NAME"
    else
        project_name=$(compose_env_last_value COMPOSE_PROJECT_NAME)
        persisted_status=$?
        case "$persisted_status" in
            0)
                if [ -z "$project_name" ]; then
                    project_name=$(basename "$PROJECT_ROOT")
                fi
                ;;
            1)
                project_name=$(basename "$PROJECT_ROOT")
                ;;
            *)
                echo "Error: COMPOSE_PROJECT_NAME in $ENV_FILE uses unsupported or malformed Compose dotenv syntax." >&2
                return 1
                ;;
        esac
    fi

    if [[ ! "$project_name" =~ ^[a-z0-9][a-z0-9_-]*$ ]]; then
        echo "Error: COMPOSE_PROJECT_NAME must start with a lowercase letter or digit and contain only lowercase letters, digits, hyphens, and underscores." >&2
        return 1
    fi

    printf '%s' "$project_name"
}
