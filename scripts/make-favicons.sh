#!/usr/bin/env bash
# Generates a favicon set from a square logo badge.
#
# The source logos are round badges sitting on an opaque white square. White is
# also used inside the artwork, so the corners are removed with a geometric
# circular mask derived from the badge's bounding box rather than by colour.
set -euo pipefail

SIZES=(16 32 48 64 96 128 256 512)
ICO_SIZES=48,32,16

usage() {
	echo "usage: $0 <logo.png> <output-dir>" >&2
	exit 2
}

[ $# -eq 2 ] || usage
src=$1
out=$2

command -v magick >/dev/null || {
	echo "ImageMagick (magick) is required" >&2
	exit 1
}

mkdir -p "$out"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Bounding box of everything that is not the white backdrop. The here-string
# supplies the newline that -format omits, which read needs to succeed.
bbox=$(magick "$src" -bordercolor white -fuzz 3% -trim -format '%w %h %X %Y' info: | tr -d '+')
read -r bw bh bx by <<< "$bbox"

side=$((bw < bh ? bw : bh))
cx=$((bx + bw / 2))
cy=$((by + bh / 2))

magick "$src" -crop "${side}x${side}+$((cx - side / 2))+$((cy - side / 2))" \
	+repage "$work/base.png"

# The badge fills the crop, so the inscribed circle is its outline.
magick -size "${side}x${side}" xc:none -fill white \
	-draw "circle $((side / 2)),$((side / 2)) $((side / 2)),0" "$work/mask.png"
magick "$work/base.png" "$work/mask.png" \
	-alpha off -compose CopyOpacity -composite "$work/circle.png"

# Colour just inside the badge edge, used where transparency is unwanted.
bg=$(magick "$work/base.png" -format "%[pixel:p{$((side / 2)),$((side * 6 / 100))}]" info:)

for size in "${SIZES[@]}"; do
	magick "$work/circle.png" -filter Lanczos -resize "${size}x${size}" \
		-strip "$out/favicon-${size}x${size}.png"
done

magick "$work/circle.png" -filter Lanczos -resize 256x256 \
	-define "icon:auto-resize=${ICO_SIZES}" "$out/favicon.ico"

# iOS renders transparency as black, so the touch icon keeps an opaque backdrop.
magick "$work/circle.png" -filter Lanczos -resize 180x180 \
	-background "$bg" -alpha remove -alpha off -strip "$out/apple-touch-icon.png"

# Maskable icons get cropped to a shape by the launcher, so the badge is inset
# into the 80% safe zone over a full-bleed background.
for size in 192 512; do
	magick "$work/circle.png" -filter Lanczos -resize "$((size * 8 / 10))x$((size * 8 / 10))" \
		-background "$bg" -gravity center -extent "${size}x${size}" \
		-alpha remove -alpha off -strip "$out/icon-${size}-maskable.png"
done

count=$(find "$out" -type f | wc -l | tr -d ' ')
echo "$(basename "$src"): badge ${side}px, background ${bg}, ${count} files in $out"
