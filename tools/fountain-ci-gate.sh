#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
set -euo pipefail
for lane in SOURCE UNIT DATABASE BROWSER PACKAGING; do
  variable="${lane}_RESULT"
  if [[ ${!variable:-missing} != success ]]; then
    printf '%s must succeed (received %s)\n' "$lane" "${!variable:-missing}" >&2
    exit 1
  fi
done
