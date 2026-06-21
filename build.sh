#!/bin/sh
set -eu

trimpath="-trimpath"
goproxy=""
skip_admin_ui=0

while [ "$#" -gt 0 ]; do
	case "$1" in
		--goproxy)
			[ "$#" -ge 2 ] || { echo "--goproxy requires a value" >&2; exit 1; }
			goproxy="$2"
			shift 2
			;;
		--no-trimpath)
			trimpath=""
			shift
			;;
		--skip-admin-ui)
			skip_admin_ui=1
			shift
			;;
		-h|--help)
			echo "Usage: $0 [--goproxy <value>] [--no-trimpath] [--skip-admin-ui]"
			exit 0
			;;
		*)
			echo "unknown option: $1" >&2
			exit 1
			;;
	esac
done

if [ -n "$goproxy" ]; then
	export GOPROXY="$goproxy"
fi

if [ "$skip_admin_ui" -eq 0 ] && [ -f internal/admin/web/package.json ]; then
	if ! command -v npm >/dev/null 2>&1; then
		echo "npm is required to build the admin UI; use --skip-admin-ui only when internal/admin/static is already built" >&2
		exit 1
	fi
	(
		cd internal/admin/web
		if [ -f package-lock.json ]; then
			npm ci
		else
			npm install
		fi
		npm run build
	)
fi

go build $trimpath -ldflags="-s -w" -o apk-cache ./cmd/apk-cache
