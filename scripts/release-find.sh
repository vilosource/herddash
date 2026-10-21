#!/usr/bin/env bash
# release-find.sh decides which release, if any, .github/workflows/release.yml
# builds binaries for.
#
#   scripts/release-find.sh workflow_run <head-sha> <force>
#   scripts/release-find.sh workflow_dispatch <tag> <force>
#
# It runs inside a clone with HEAD checked out at the commit to build and the
# tags fetched, and asks GitHub through `gh` (GH_TOKEN and GH_REPO set).
#
# workflow_run: looks for a v* tag on <head-sha> that has a GitHub release. None
# is the ordinary push: it prints found=false and exits 0. More than one is an
# error.
# workflow_dispatch: <tag> must have a GitHub release.
#
# Either way the tag must be v<X.Y.Z>[-<prerelease>], point at HEAD, and its
# release must have no assets yet unless <force> is true. On success it prints
# key=value lines for $GITHUB_OUTPUT: found, tag, version (no v) and commit (12
# characters). Diagnostics go to stderr. Any failure exits non-zero; only gh's
# "release not found" counts as no release.
set -euo pipefail

usage() {
	echo "usage: $0 workflow_run|workflow_dispatch <head-sha|tag> true|false" >&2
	exit 2
}
fail() {
	echo "$0: $*" >&2
	exit 1
}

[ "$#" -eq 3 ] || usage
event="$1"
ref="$2"
force="$3"
case "$force" in true | false) ;; *) usage ;; esac
[ -n "$ref" ] || usage

# The same rule as scripts/release-build.sh's version, with a v.
tag_re='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'

# assets <tag> prints the number of assets on the tag's release. It returns 1
# when gh says the release does not exist and 2 on any other failure.
assets() {
	local out rc=0
	out="$(gh release view "$1" --json assets --jq '.assets | length' 2>&1)" || rc=$?
	if [ "$rc" -eq 0 ]; then
		if [[ "$out" =~ ^[0-9]+$ ]]; then
			printf '%s\n' "$out"
			return 0
		fi
		echo "gh release view $1 printed $out, not an asset count" >&2
		return 2
	fi
	if [[ "$out" == *"release not found"* ]]; then
		return 1
	fi
	echo "gh release view $1 failed: $out" >&2
	return 2
}

head="$(git rev-parse --verify HEAD^{commit})"
tag=""
count=""
case "$event" in
workflow_run)
	[[ "$ref" =~ ^[0-9a-f]{40}$ ]] || fail "head sha $ref is not a 40-character commit"
	[ "$head" = "$ref" ] || fail "HEAD is $head, not the run's head commit $ref"
	for t in $(git tag --points-at "$ref" --list 'v*'); do
		rc=0
		n="$(assets "$t")" || rc=$?
		case "$rc" in
		0)
			[ -z "$tag" ] || fail "tags $tag and $t on $ref both have a release"
			tag="$t"
			count="$n"
			;;
		1) ;;
		*) fail "could not look up the release for $t" ;;
		esac
	done
	if [ -z "$tag" ]; then
		echo "no v* tag with a release on $ref; nothing to do" >&2
		echo "found=false"
		exit 0
	fi
	;;
workflow_dispatch)
	tag="$ref"
	[[ "$tag" =~ $tag_re ]] || fail "tag $tag is not v<X.Y.Z>[-<prerelease>]"
	rc=0
	count="$(assets "$tag")" || rc=$?
	case "$rc" in
	0) ;;
	1) fail "no GitHub release for tag $tag" ;;
	*) fail "could not look up the release for $tag" ;;
	esac
	;;
*)
	usage
	;;
esac

[[ "$tag" =~ $tag_re ]] || fail "tag $tag is not v<X.Y.Z>[-<prerelease>]"
target="$(git rev-parse --verify "refs/tags/$tag^{commit}")" || fail "tag $tag is not in this clone"
[ "$target" = "$head" ] || fail "tag $tag points at $target, not HEAD $head"
if [ "$count" -gt 0 ] && [ "$force" != true ]; then
	fail "release $tag already has $count assets; rerun with force=true to replace them"
fi

echo "found=true"
echo "tag=$tag"
echo "version=${tag#v}"
echo "commit=$(git rev-parse --short=12 HEAD)"
