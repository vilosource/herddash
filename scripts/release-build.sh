#!/usr/bin/env bash
# release-build.sh builds herddash's release binaries and their checksums.
#
#   scripts/release-build.sh <version> <commit> <outdir>
#
# For each of darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64 it builds
# a binary with CGO_ENABLED=0 and -trimpath, the version and commit stamped in,
# named herddash_<version>_<os>_<arch>, then writes one SHA256SUMS file covering
# every binary. <version> is X.Y.Z with an optional -prerelease of [0-9A-Za-z.-];
# a leading "v" is dropped. It runs the same way locally and in
# .github/workflows/release.yml, from anywhere in the repository, and writes
# nothing outside <outdir> (Go's build cache aside).
#
# HERDDASH_TARGETS, a space-separated list of os/arch pairs, narrows the build
# to those targets; the release always builds all four.
set -euo pipefail

usage() {
	echo "usage: $0 <version> <commit> <outdir>" >&2
	exit 2
}

[ "$#" -eq 3 ] || usage
version="${1#v}"
commit="$2"
outdir="$3"
[ -n "$outdir" ] || usage
# X.Y.Z with an optional -prerelease; scripts/release-find.sh's tag_re is this
# rule with a v, and release_test.go holds them equal.
version_re='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
if ! [[ "$version" =~ $version_re ]]; then
	echo "$0: version $1 is not [v]X.Y.Z[-prerelease]" >&2
	exit 2
fi
if ! [[ "$commit" =~ ^[0-9A-Za-z][0-9A-Za-z.-]*$ ]]; then
	echo "$0: commit $commit may hold only letters, digits, . and -" >&2
	exit 2
fi

all_targets="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64"
targets="$all_targets"
if [ -n "${HERDDASH_TARGETS+set}" ]; then
	targets="$HERDDASH_TARGETS"
	if [ -z "${targets//[[:space:]]/}" ]; then
		echo "$0: HERDDASH_TARGETS is set but blank" >&2
		exit 2
	fi
fi
seen=" "
for t in $targets; do
	case " $all_targets " in
	*" $t "*) ;;
	*)
		echo "$0: unknown target $t (want one of: $all_targets)" >&2
		exit 2
		;;
	esac
	case "$seen" in
	*" $t "*)
		echo "$0: target $t is named twice" >&2
		exit 2
		;;
	esac
	seen="$seen$t "
done

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$@"; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$@"; }
else
	echo "$0: neither sha256sum nor shasum is installed" >&2
	exit 1
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$outdir"
outdir="$(cd "$outdir" && pwd)"
# A stale SHA256SUMS must never survive a failed build.
rm -f "$outdir/SHA256SUMS" "$outdir/SHA256SUMS.tmp"

ldflags="-s -w -X main.version=$version -X main.commit=$commit"
names=()
for t in $targets; do
	os="${t%/*}"
	arch="${t#*/}"
	name="herddash_${version}_${os}_${arch}"
	echo "building $name" >&2
	(cd "$root" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
		go build -trimpath -ldflags "$ldflags" -o "$outdir/$name" ./cmd/herddash)
	names+=("$name")
done

(cd "$outdir" && sha256 "${names[@]}" > SHA256SUMS.tmp && mv SHA256SUMS.tmp SHA256SUMS)
echo "wrote ${#names[@]} binaries and SHA256SUMS to $outdir" >&2
