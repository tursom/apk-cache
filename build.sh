#!/bin/sh
set -eu

trimpath="-trimpath"
goproxy=""

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
		-h|--help)
			echo "Usage: $0 [--goproxy <value>] [--no-trimpath]"
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

go build $trimpath -ldflags="-s -w" -o apk-cache ./cmd/apk-cache
