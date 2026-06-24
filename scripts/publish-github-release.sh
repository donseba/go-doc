#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${ROOT}/dist"
TAG="${1:-${CIRCLE_TAG:-}}"
REPO="${GITHUB_REPOSITORY:-${CIRCLE_PROJECT_USERNAME:-donseba}/${CIRCLE_PROJECT_REPONAME:-go-doc}}"

if [[ -z "${TAG}" ]]; then
  echo "release tag is required, for example v0.1.0" >&2
  exit 1
fi

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "GITHUB_TOKEN is required to publish GitHub release assets" >&2
  exit 1
fi

if [[ ! -d "${DIST}" ]]; then
  echo "dist directory does not exist; run scripts/build-dist.sh first" >&2
  exit 1
fi

api="https://api.github.com/repos/${REPO}"
auth_header="Authorization: Bearer ${GITHUB_TOKEN}"
api_header="Accept: application/vnd.github+json"
version_header="X-GitHub-Api-Version: 2022-11-28"

release_json="$(
  curl -fsS \
    -H "${auth_header}" \
    -H "${api_header}" \
    -H "${version_header}" \
    "${api}/releases/tags/${TAG}" || true
)"

if [[ -z "${release_json}" ]]; then
  release_json="$(
    jq -n --arg tag "${TAG}" --arg name "go-doc ${TAG}" \
      '{tag_name: $tag, name: $name, draft: false, prerelease: false}' |
    curl -fsS -X POST \
      -H "${auth_header}" \
      -H "${api_header}" \
      -H "${version_header}" \
      -H "Content-Type: application/json" \
      --data-binary @- \
      "${api}/releases"
  )"
fi

upload_url="$(jq -r '.upload_url' <<<"${release_json}" | sed 's/{?name,label}$//')"
release_id="$(jq -r '.id' <<<"${release_json}")"

if [[ -z "${upload_url}" || "${upload_url}" == "null" || -z "${release_id}" || "${release_id}" == "null" ]]; then
  echo "could not resolve GitHub release upload URL for ${TAG}" >&2
  exit 1
fi

assets_json="$(
  curl -fsS \
    -H "${auth_header}" \
    -H "${api_header}" \
    -H "${version_header}" \
    "${api}/releases/${release_id}/assets"
)"

while IFS= read -r file; do
  name="$(basename "${file}")"
  existing_id="$(jq -r --arg name "${name}" '.[] | select(.name == $name) | .id' <<<"${assets_json}" | head -n 1)"

  if [[ -n "${existing_id}" && "${existing_id}" != "null" ]]; then
    curl -fsS -X DELETE \
      -H "${auth_header}" \
      -H "${api_header}" \
      -H "${version_header}" \
      "${api}/releases/assets/${existing_id}" >/dev/null
  fi

  curl -fsS -X POST \
    -H "${auth_header}" \
    -H "${api_header}" \
    -H "${version_header}" \
    -H "Content-Type: application/octet-stream" \
    --data-binary @"${file}" \
    "${upload_url}?name=${name}" >/dev/null

  echo "uploaded ${name}"
done < <(find "${DIST}" -maxdepth 1 -type f | sort)
