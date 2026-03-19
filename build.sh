#!/usr/bin/env sh

set -eu

usage() {
	cat <<'EOF'
Usage: ./build.sh [OPTIONS]

Options:
  -o, --output-dir DIR  Directory to place binaries (default: ./dist/native or ./dist/PLATFORM)
  -a, --arch ARCH       Target architecture (e.g., amd64, arm64, 386)
  -s, --os OS           Target operating system (e.g., linux, windows, darwin)
  --all                 Build for all common platforms (linux/amd64, linux/arm64, windows/amd64, windows/arm64, darwin/amd64, darwin/arm64)
  -h, --help            Show this help message

Builds the native host binaries for this repository:
  - aubar
  - quota
  - gemini-quota
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

detect_arch() {
	m=$(uname -m 2>/dev/null || echo "unknown")
	case "$m" in
		x86_64|amd64) echo "amd64" ;;
		i386|i686|x86) echo "386" ;;
		aarch64|arm64) echo "arm64" ;;
		armv7*) echo "arm" ;;
		*) echo "$m" ;;
	esac
}

SCRIPT_DIR=$(
	CDPATH= cd -- "$(dirname -- "$0")" && pwd
)
REPO_ROOT=$SCRIPT_DIR
OUTPUT_DIR=""
TARGET_ARCH=""
TARGET_OS=""
BUILD_ALL=false

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
		-a|--arch)
			TARGET_ARCH=$2
			shift 2
			;;
		-s|--os)
			TARGET_OS=$2
			shift 2
			;;
		--all)
			BUILD_ALL=true
			shift
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
DEFAULT_OS=$(go env GOHOSTOS | tr -d '\r')
DEFAULT_ARCH=$(go env GOHOSTARCH | tr -d '\r')

# Fallback architecture detection if go env fails or isn't what we want
if [ -z "$DEFAULT_ARCH" ]; then
	DEFAULT_ARCH=$(detect_arch)
fi

build_one() {
	name=$1
	pkg=$2
	go_os=$3
	go_arch=$4
	out_dir=$5

	exe_suffix=""
	if [ "$go_os" = "windows" ]; then
		exe_suffix=".exe"
	fi

	go_out_dir=$(normalize_for_go "$out_dir")
	out_path=$go_out_dir/$name$exe_suffix

	printf '==> building %s for %s/%s\n' "$name" "$go_os" "$go_arch"
	(
		cd "$REPO_ROOT"
		# Disable CGO for maximum portability unless we're on a platform that really needs it
		# For keyring support, go-keyring usually works without CGO on most platforms.
		CGO_ENABLED=0 GOOS=$go_os GOARCH=$go_arch go build -trimpath -o "$out_path" "$pkg"
	)
}

build_platform() {
	os=$1
	arch=$2
	
	platform_out_dir="$OUTPUT_DIR"
	if [ -z "$platform_out_dir" ]; then
		if [ "$BUILD_ALL" = true ]; then
			platform_out_dir="$REPO_ROOT/dist/$os-$arch"
		else
			platform_out_dir="$REPO_ROOT/dist/native"
		fi
	fi

	platform_out_dir=$(abspath "$platform_out_dir")
	mkdir -p "$platform_out_dir"

	build_one "aubar" "./cmd/aubar" "$os" "$arch" "$platform_out_dir"
	build_one "quota" "./cmd/quota" "$os" "$arch" "$platform_out_dir"
	build_one "gemini-quota" "./cmd/gemini-quota" "$os" "$arch" "$platform_out_dir"
}

if [ "$BUILD_ALL" = true ]; then
	build_platform "linux" "amd64"
	build_platform "linux" "arm64"
	build_platform "linux" "386"
	build_platform "windows" "amd64"
	build_platform "windows" "arm64"
	build_platform "windows" "386"
	build_platform "darwin" "amd64"
	build_platform "darwin" "arm64"
else
	os=${TARGET_OS:-$DEFAULT_OS}
	arch=${TARGET_ARCH:-$DEFAULT_ARCH}
	build_platform "$os" "$arch"
fi

if [ "$BUILD_ALL" = true ]; then
	printf 'built all binaries in %s/dist/\n' "$REPO_ROOT"
else
	printf 'built binaries in %s\n' "${OUTPUT_DIR:-$REPO_ROOT/dist/native}"
fi
