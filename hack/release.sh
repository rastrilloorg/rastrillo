#!/bin/sh
# Publish a tag as a release on both forges.
#
#   hack/release.sh vX.Y.Z...      publish each tag on amadan and GitHub
#   hack/release.sh -n vX.Y.Z...   build and print, publish nothing
#   hack/release.sh -check         list tags whose release is missing or
#                                  incomplete
#
# A release is the tag's CHANGELOG section as notes, plus the CLI for
# four platforms as tarballs and a checksums.txt. Doc links in the notes
# point at rastrillo.org, because a relative /docs/ link is broken on
# both forges.
#
# amadan has no verb to delete a release, edit its notes or replace a
# file, so a wrong release there is permanent. Everything that can fail
# runs before the first publish: the tag must be on origin, the notes
# must be non-empty, and every binary's build info must name the tag.
#
# A forge can end up holding part of a release: the release is created
# before its files are uploaded, and an upload can fail. So a re-run
# compares each forge's file list with the full set and uploads only
# what is missing. The files a forge already has must be byte for byte
# the ones about to join them, or checksums.txt would describe other
# files. The build is reproducible (fixed tar metadata, gzip -n,
# -trimpath) for that, and the script checks it rather than trusting
# it: an existing file is downloaded and compared, and a difference
# stops the run. When one forge already has every file (amadan's
# v0.28.0, built before this script), those files are copied to the
# other instead of building new ones.
#
# The notes come from the CHANGELOG at the tag, or from origin/main's
# when the tag's has no section for it (v0.25.0 was tagged with no
# prep). A tag with neither is refused: everything before v0.23.0,
# where the CHANGELOG starts.
set -eu

AMADAN_REPO=${AMADAN_REPO:-rastrillo/rastrillo}
GITHUB_REPO=${GITHUB_REPO:-rastrilloorg/rastrillo}
MIRROR_REMOTE=${MIRROR_REMOTE:-github}
HUB=https://amadan.net
DOCS=https://rastrillo.org
PLATFORMS="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64"

die() { echo "release: $*" >&2; exit 1; }

# section prints tag's section of the CHANGELOG at rev, without its
# heading, HTML comments (notes to the gate, not to readers) or the
# blank lines around it.
section() {
	git show "$2:CHANGELOG.md" 2>/dev/null | awk -v t="$1" '
		/^## / { if (on) exit; on = ($2 == t); next }
		on' | perl -0pe 's/<!--.*?-->\n?//gs' |
		sed -e "s#](/docs/#]($DOCS/docs/#g" |
		awk 'NF { for (; blank > 0; blank--) print ""; print; seen = 1; next }
		     seen { blank++ }'
}

notes() {
	n=$(section "$1" "$1")
	[ -n "$n" ] || n=$(section "$1" refs/remotes/origin/main)
	[ -z "$n" ] || printf '%s\n' "$n"
}

# files lists the names a complete release of tag carries.
files() {
	for p in $PLATFORMS; do echo "rastrillo_$1_${p%/*}_${p#*/}.tar.gz"; done
	echo checksums.txt
}

# amadan_has and github_has print the files tag's release holds on that
# forge. They return 3 if there is no release, and anything else but 0
# means the forge could not be asked: that must not read as "no
# release", or a hiccup would publish a second time.
amadan_has() {
	page=$(curl -s -w '\n%{http_code}' "$HUB/$AMADAN_REPO/-/releases/$1")
	case $(printf '%s' "$page" | tail -n1) in
	200) ;;
	404) return 3 ;;
	*) die "$HUB answered $(printf '%s' "$page" | tail -n1) for $1's release page" ;;
	esac
	printf '%s' "$page" | grep -o "/-/releases/$1/[A-Za-z0-9._-]*\"" |
		sed 's#.*/##; s#"$##' | sort -u
}
github_has() {
	if ! out=$(gh release view "$1" -R "$GITHUB_REPO" --json assets \
		-q '.assets[].name' 2>&1); then
		case $out in
		*"release not found"*) return 3 ;;
		*) die "gh release view $1: $out" ;;
		esac
	fi
	[ -z "$out" ] || printf '%s\n' "$out"
}

# lacking prints the files of tag's full set that are not in the list
# on stdin.
lacking() {
	have=$(cat)
	for f in $(files "$1"); do
		printf '%s\n' "$have" | grep -qx "$f" || echo "$f"
	done
	return 0
}

# released lists the tags that should have a release: every v tag whose
# CHANGELOG has a section for it.
released() {
	for t in $(git tag -l 'v*' --sort=v:refname); do
		[ -n "$(notes "$t")" ] && echo "$t"
	done
	return 0
}

# state prints what forge's release of tag is missing: "none" for no
# release, the missing file names, or nothing when it is complete.
state() {
	rc=0
	has=$("$1_has" "$2") || rc=$?
	case $rc in
	0) printf '%s\n' "$has" | lacking "$2" ;;
	3) echo none ;;
	*) die "could not read $2's release on $1" ;;
	esac
}

check() {
	git fetch -q --tags origin
	git fetch -q origin main
	bad=0
	for t in $(released); do
		for forge in amadan github; do
			s=$(state $forge "$t")
			case $s in
			"") ;;
			none) echo "$t: no release on $forge"; bad=1 ;;
			*) echo "$t: $forge lacks" $s; bad=1 ;;
			esac
		done
	done
	if [ $bad = 1 ]; then
		echo
		echo "publish them with: make release TAG=vX.Y.Z"
		exit 1
	fi
	echo "every tag with a CHANGELOG section has a full release on both forges"
}

# build writes the tarballs and checksums.txt for tag into dir. It
# builds the way the README installs, go install of the module at the
# tag, so each binary's build info names the tag. A go build in a
# checkout of this repo stamps no version (Go drops the VCS info here)
# and the CLI would fall back to its constant. The module path is read
# from the tag, since releases before v0.25.0 were under GitHub's path.
# GOPATH is the scratch directory, so nothing lands in your own bin;
# GOMODCACHE stays shared.
build() {
	t=$1 dir=$2
	mod=$(git show "$t:go.mod" | awk '$1 == "module" { print $2; exit }')
	when=$(git log -1 --format=%ct "$t")
	for p in $PLATFORMS; do
		os=${p%/*} arch=${p#*/}
		CGO_ENABLED=0 GOOS=$os GOARCH=$arch GOFLAGS=-trimpath \
			GOMODCACHE=$(go env GOMODCACHE) GOPATH=$dir/gopath \
			go install "$mod/cmd/rastrillo@$t"
		bin=$dir/gopath/bin/${os}_$arch/rastrillo
		[ -f "$bin" ] || bin=$dir/gopath/bin/rastrillo
		name=rastrillo_${t}_${os}_${arch}
		mkdir "$dir/$name"
		mv "$bin" "$dir/$name/"
		git show "$t:LICENSE" >"$dir/$name/LICENSE"
		chmod 644 "$dir/$name/LICENSE"
		chmod 755 "$dir/$name" "$dir/$name/rastrillo"
		got=$(go version -m "$dir/$name/rastrillo" |
			awk '$1 == "mod" { print $3 }')
		[ "$got" = "$t" ] ||
			die "$name says it is $got, not $t; the build lost its version"
		tar -C "$dir" --sort=name --owner=0 --group=0 --numeric-owner \
			--mtime="@$when" -cf - "$name" | gzip -n -9 >"$dir/$name.tar.gz"
		rm -r "$dir/$name"
	done
	chmod -R u+w "$dir/gopath"
	rm -rf "$dir/gopath"
	(cd "$dir" && sha256sum $(files "$t" | grep -v checksums) >checksums.txt)
}

# fetch downloads forge's copy of tag's file into dir.
fetch() {
	case $1 in
	amadan) curl -fsSL -o "$4/$3" "$HUB/$AMADAN_REPO/-/releases/$2/$3" ;;
	github) gh release download "$2" -R "$GITHUB_REPO" -p "$3" -D "$4" ;;
	esac
}

# agree stops the run unless every file forge already holds for tag is
# identical to the one in dir.
agree() {
	forge=$1 t=$2 dir=$3
	mkdir "$dir/have"
	for f in $(files "$t"); do
		printf '%s\n' "$4" | grep -qx "$f" && continue
		fetch "$forge" "$t" "$f" "$dir/have"
		cmp -s "$dir/have/$f" "$dir/$f" ||
			die "$t: $forge already has a $f that differs from this one; sort it out by hand"
	done
	rm -r "$dir/have"
}

publish() {
	t=$1 dry=$2
	o=$(git ls-remote origin "refs/tags/$t" | cut -f1)
	[ -n "$o" ] || die "$t is not on origin; push the tag first"
	m=$(git ls-remote "$MIRROR_REMOTE" "refs/tags/$t" | cut -f1)
	[ -z "$m" ] || [ "$m" = "$o" ] ||
		die "$t is $o on origin and $m on $MIRROR_REMOTE"
	git fetch -q origin main "refs/tags/$t:refs/tags/$t"

	a=$(state amadan "$t")
	g=$(state github "$t")
	if [ -z "$a" ] && [ -z "$g" ]; then
		echo "$t: already complete on both forges"
		return
	fi

	dir=$(mktemp -d)
	if [ "$dry" = 0 ]; then
		trap 'rm -rf "$dir"' EXIT
		trap 'exit 130' INT TERM
	fi
	notes "$t" >"$dir/notes.md"
	[ -s "$dir/notes.md" ] || die "$t has no section in its CHANGELOG"
	if [ -z "$a" ] || [ -z "$g" ]; then
		from=amadan
		[ -z "$a" ] || from=github
		for f in $(files "$t"); do fetch $from "$t" "$f" "$dir"; done
		(cd "$dir" && sha256sum -c --quiet checksums.txt) ||
			die "$t: $from's files do not match its checksums.txt"
		echo "$t: copying $from's files"
	else
		build "$t" "$dir"
	fi
	# A partial release must already hold these very files.
	[ "$a" = none ] || [ -z "$a" ] || agree amadan "$t" "$dir" "$a"
	[ "$g" = none ] || [ -z "$g" ] || agree github "$t" "$dir" "$g"

	if [ "$dry" = 1 ]; then
		echo "$t: would publish from $dir"
		echo "  amadan lacks: ${a:-nothing}"
		echo "  GitHub lacks: ${g:-nothing}"
		ls -l "$dir"
		return
	fi

	[ -n "$m" ] || git push "$MIRROR_REMOTE" "refs/tags/$t"

	case $a in
	"") ;;
	none) set -- $(files "$t")
		(cd "$dir" && amadan release create -repo "$AMADAN_REPO" \
			-notes-file notes.md "$t" "$@") ;;
	*) set -- $a
		(cd "$dir" && amadan release create -repo "$AMADAN_REPO" "$t" "$@") ;;
	esac

	# GitHub marks one release Latest. Without saying so, backfilling an
	# old tag would take it from the newest.
	newest=$(git tag -l 'v*' --sort=-v:refname | head -1)
	latest=false
	[ "$t" = "$newest" ] && latest=true
	case $g in
	"") ;;
	none) set -- $(files "$t")
		(cd "$dir" && gh release create "$t" -R "$GITHUB_REPO" --verify-tag \
			--title "$t" --notes-file notes.md --latest=$latest "$@") ;;
	*) set -- $g
		(cd "$dir" && gh release upload "$t" -R "$GITHUB_REPO" "$@") ;;
	esac
	# gh leaves a draft behind when an upload fails during create.
	if [ "$(gh release view "$t" -R "$GITHUB_REPO" --json isDraft -q .isDraft)" = true ]; then
		gh release edit "$t" -R "$GITHUB_REPO" --draft=false --latest=$latest
	fi

	rm -rf "$dir"
	trap - EXIT INT TERM
}

dry=0
case ${1:-} in
-check) check; exit ;;
-n) dry=1; shift ;;
esac
[ $# -gt 0 ] || die "usage: hack/release.sh [-n] vX.Y.Z... | -check"
for t; do publish "$t" $dry; done
