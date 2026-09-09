#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd "${script_dir}/.." && pwd)"

pvf_file="${PVF_TESTFILE:-${repo_dir}/Script.pvf}"
npk_dir="${NPK_TESTDIR:-${repo_dir}/ImagePacks2}"

if [[ "${pvf_file}" != /* ]]; then
  pvf_file="${repo_dir}/${pvf_file}"
fi
if [[ "${npk_dir}" != /* ]]; then
  npk_dir="${repo_dir}/${npk_dir}"
fi

if [[ ! -f "${pvf_file}" ]]; then
  echo "找不到 PVF 资源: ${pvf_file}" >&2
  exit 1
fi
if [[ ! -d "${npk_dir}" ]]; then
  echo "找不到 NPK 目录: ${npk_dir}" >&2
  exit 1
fi

echo "PVF: ${pvf_file}"
echo "NPK: ${npk_dir}"
PVF_TESTFILE="${pvf_file}" NPK_TESTDIR="${npk_dir}" \
  go test -run '^$' -bench '^BenchmarkRealResources$' -benchtime=1x -count=1 -v ./services
