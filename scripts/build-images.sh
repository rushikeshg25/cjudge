#!/usr/bin/env sh
set -eu
for language in cpp python go; do
  docker build -t "cjudge-${language}:1" "images/${language}"
done
