#!/bin/bash

# git-sv uses go-git underneath, and go-git at the moment cannot authenticate properly
# in GitHub Actions ( see https://github.com/go-git/go-git/issues/474). Hence the script

NEXT_VERSION=$(git sv next-version)

if [[ "$NEXT_VERSION" == "" ]]; then
  echo "No new version found, exiting"
  exit 0
fi

echo "New version $NEXT_VERSION found, creating the tag and pushing to remote"

git tag "v${NEXT_VERSION}"
git push origin "v${NEXT_VERSION}"

goreleaser release