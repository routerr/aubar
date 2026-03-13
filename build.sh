#!/usr/bin/env sh

set -eu

usage() {
	cat <<'EOF'
Usage: ./build.sh [--output-dir DIR]

Builds the native host binaries for this repository:
  - aubar
  - quota
  - gemini-quota

Examples:
  ./build.sh
  ./build.sh --output-dir ./dist/custom
EOF
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf 'missing required command: %s\n' "$1" >&2
		exit 127
	fi
}

abspath() {
	case "$1" in
		/*) printf '%s\n' "$1" ;;
		[A-Za-z]:[\\/]* ) printf '%s\n' "$1" ;;
		*) printf '%s/%s\n' "$REPO_ROOT" "$1" ;;
	esac
}

normalize_for_go() {
	path_value=$1
	case "$HOST_UNAME" in
		msys*|mingw*|cygwin*)
			if command -v cygpath >/dev/null 2>&1; then
				cygpath -m "$path_value"
				return
			fi
			;;
	esac
	printf '%s\n' "$path_value"
}

SCRIPT_DIR=$(
	CDPATH= cd -- "$(dirname -- "$0")" && pwd
)
REPO_ROOT=$SCRIPT_DIR
OUTPUT_DIR="$REPO_ROOT/dist/native"

while [ "$#" -gt 0 ]; do
	case "$1" in
		-o|--output-dir)
			if [ "$#" -lt 2 ]; then
				printf 'missing value for %s\n' "$1" >&2
				usage >&2
				exit 2
			fi
			OUTPUT_DIR=$2
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			printf 'unknown argument: %s\n' "$1" >&2
			usage >&2
			exit 2
			;;
	esac
done

require_command go

HOST_UNAME=$(uname -s 2>/dev/null | tr '[:upper:]' '[:lower:]')
HOST_GOOS=$(go env GOHOSTOS | tr -d '\r')
HOST_GOARCH=$(go env GOHOSTARCH | tr -d '\r')

OUTPUT_DIR=$(abspath "$OUTPUT_DIR")
mkdir -p "$OUTPUT_DIR"

GO_OUTPUT_DIR=$(normalize_for_go "$OUTPUT_DIR")
EXE_SUFFIX=""
if [ "$HOST_GOOS" = "windows" ]; then
	EXE_SUFFIX=".exe"
fi

build_one() {
	name=$1
	pkg=$2
	out_path=$GO_OUTPUT_DIR/$name$EXE_SUFFIX
	printf '==> building %s for %s/%s\n' "$name" "$HOST_GOOS" "$HOST_GOARCH"
	(
		cd "$REPO_ROOT"
		GOOS=$HOST_GOOS GOARCH=$HOST_GOARCH go build -trimpath -o "$out_path" "$pkg"
	)
}

build_one "aubar" "./cmd/aubar"
build_one "quota" "./cmd/quota"
build_one "gemini-quota" "./cmd/gemini-quota"

printf 'built binaries in %s\n' "$OUTPUT_DIR"
