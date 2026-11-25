#!/bin/sh
# Publish a tag as a release on both forges.
#
#   hack/release.sh vX.Y.Z...      publish each tag on amadan and GitHub
#   hack/release.sh -n vX.Y.Z...   build and print, publish nothing
#   hack/release.sh -sign CMD ...  sign checksums.txt with CMD (below)
#   hack/release.sh -check         list tags whose release is missing or
#                                  incomplete
#   hack/release.sh -files vX.Y.Z  print the files tag's release carries
#   hack/release.sh -sign CMD -try-sign
#                                  sign and verify a scratch file with CMD
#   hack/release.sh -tidy-sbom F   drop the SBOM tool's hashes of itself
#
# A release is the tag's CHANGELOG section as notes, plus the CLI for
# four platforms as tarballs, a CycloneDX SBOM for each binary and one
# for the module, and a checksums.txt over all of them. Doc links in the
# notes point at rastrillo.org, because a relative /docs/ link is broken
# on both forges.
#
# The signature covers checksums.txt, which covers everything else. CMD
# is run as `CMD sign checksums.txt SIG` to write the signature and as
# `CMD verify checksums.txt SIG` to check it, so the key's home can
# change without touching this script. Which method signs is still open
# (discussion #417); until SIGN_AFTER names a tag, no release needs a
# signature and -sign is only rehearsed, by -n or -try-sign. Both really
# sign, so a keyless signer leaves an entry in its public log.
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
# go run's pin, not a tool directive, for the reason the Makefile gives
# staticcheck: a tool's requirements in go.mod join every app's graph.
CYCLONEDX=github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.12.0

# SBOM_AFTER and SIGN_AFTER are the newest tags released without an SBOM
# and without a signature. A published release cannot gain a file:
# checksums.txt would no longer list everything beside it, and amadan
# cannot replace it. Without the cut, release-check would report every
# older release as incomplete and release would try to add to them.
SBOM_AFTER=${SBOM_AFTER:-v0.47.0}
SIGN_AFTER=${SIGN_AFTER:-}
SIGNATURE=${SIGNATURE:-checksums.txt.sig}

die() { echo "release: $*" >&2; exit 1; }

# newer reports whether tag $1 comes after tag $2.
newer() {
	[ "$1" != "$2" ] &&
		[ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n1)" = "$1" ]
}
has_sbom() { newer "$1" "$SBOM_AFTER"; }
signed() { [ -n "$SIGN_AFTER" ] && newer "$1" "$SIGN_AFTER"; }

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

# payload lists the files of tag's release that checksums.txt covers.
payload() {
	for p in $PLATFORMS; do echo "rastrillo_$1_${p%/*}_${p#*/}.tar.gz"; done
	has_sbom "$1" || return 0
	for p in $PLATFORMS; do echo "rastrillo_$1_${p%/*}_${p#*/}.cdx.json"; done
	echo "rastrillo_$1_module.cdx.json"
}

# files lists the names a complete release of tag carries.
files() {
	payload "$1"
	echo checksums.txt
	! signed "$1" || echo "$SIGNATURE"
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
	! has_sbom "$t" || cyclonedx "$dir"
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
		! has_sbom "$t" || sbom_bin "$t" "$dir" "$name" "$os" "$arch"
		tar -C "$dir" --sort=name --owner=0 --group=0 --numeric-owner \
			--mtime="@$when" -cf - "$name" | gzip -n -9 >"$dir/$name.tar.gz"
		rm -r "$dir/$name"
	done
	chmod -R u+w "$dir/gopath"
	rm -rf "$dir/gopath"
	! has_sbom "$t" || sbom_module "$t" "$dir"
	rm -rf "$dir/tools"
	(cd "$dir" && sha256sum $(payload "$t") >checksums.txt)
}

# cyclonedx builds the SBOM tool into dir/tools. go run would do for
# the module, but the binaries' SBOMs need GOOS and GOARCH set to the
# target's: the tool puts the go env it runs under into every purl, and
# go run would build the tool itself for that target.
cyclonedx() {
	GOBIN=$1/tools GOFLAGS=-trimpath go install "$CYCLONEDX"
}

# sbom_bin writes the SBOM of the binary in dir/name, from the module
# list its build info records. One per platform, because the list
# differs: darwin links modules linux does not. No serial number or
# timestamp, so a rebuild is byte for byte the same, as agree needs.
# -std lists the Go release as a component, std@goX.Y.Z, so a scanner
# reading components sees a vulnerable runtime too; the check makes
# sure it names the Go that built the binary.
sbom_bin() {
	GOOS=$4 GOARCH=$5 "$2/tools/cyclonedx-gomod" bin -json -licenses -std \
		-noserial -notimestamp -version "$1" \
		-output "$2/$3.cdx.json" "$2/$3/rastrillo"
	tidy_sbom "$2/$3.cdx.json"
	go=$(go version "$2/$3/rastrillo" | awk '{ print $NF }')
	grep -q "\"bom-ref\": \"pkg:golang/std@$go?type=module\"" \
		"$2/$3.cdx.json" || die "$3.cdx.json does not list std@$go"
}

# tidy_sbom drops the hashes the tool records of its own executable.
# That executable is built for the release host, so the hashes differ
# between an amd64 and an arm64 host, and a release interrupted on one
# and retried on the other would fail agree on SBOMs it had already
# uploaded. The tool has no flag for it. The pattern is tied to v1.12.0's
# layout; when a bump changes it, this stops the release instead of
# shipping host-dependent SBOMs.
tidy_sbom() {
	perl -0ne '
		my $tool = q{("name": "cyclonedx-gomod",\n {8}"version": "[^"\n]*",\n)};
		s/$tool {8}"hashes": \[\n.*?\n {8}\],\n/$1/s or exit 3;
		print' "$1" >"$1.tidy" || {
		rm -f "$1.tidy"
		die "$1: the SBOM tool's own entry changed shape; update tidy_sbom"
	}
	mv "$1.tidy" "$1"
}

# sbom_module writes the SBOM of the module at tag: every module an app
# that requires it may build with. It runs in a clone because the tool
# takes the version from the clone's tag, and a shallow one because the
# tool's git reader cannot follow a --shared clone's alternates. Short
# purls: the module graph is the same on every platform.
sbom_module() {
	git -c advice.detachedHead=false clone -q --depth 1 --branch "$1" \
		"file://$(git rev-parse --show-toplevel)" "$2/src"
	(cd "$2/src" && "$2/tools/cyclonedx-gomod" mod -json -licenses \
		-type library -short-purls -noserial -notimestamp \
		-output "$2/rastrillo_$1_module.cdx.json" .)
	tidy_sbom "$2/rastrillo_$1_module.cdx.json"
	rm -rf "$2/src"
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

# seal puts tag's signature of checksums.txt in dir and verifies it. A
# signature a forge already holds is reused rather than made again: a
# keyless one differs on every run, and agree would then stop a re-run
# that only had files left to upload.
#
# -sign with -n signs for real even for a tag that needs no signature,
# so a broken signer shows up in the rehearsal rather than on release
# day. A keyless signer therefore leaves an entry in its public log.
seal() {
	t=$1 dir=$2 sig=$2/$SIGNATURE
	if signed "$t" && [ ! -f "$sig" ]; then
		held "$t" "$dir" "$3" "$4"
	fi
	if [ -z "$SIGNER" ]; then
		[ "$5" = 1 ] || die "$t needs a signature; pass -sign CMD"
		echo "$t: unsigned; pass -sign CMD to rehearse signing"
		return
	fi
	if [ ! -f "$sig" ]; then
		$SIGNER sign "$dir/checksums.txt" "$sig" ||
			die "$t: $SIGNER sign failed"
		[ -s "$sig" ] || die "$t: $SIGNER sign wrote no $SIGNATURE"
	fi
	$SIGNER verify "$dir/checksums.txt" "$sig" ||
		die "$t: $SIGNATURE does not verify against checksums.txt"
	signed "$t" ||
		echo "$t: rehearsed: $SIGNATURE signed and verified, not published"
}

# held fetches the signature a forge's partial release already has.
held() {
	for forge in amadan github; do
		case $forge in amadan) s=$3 ;; github) s=$4 ;; esac
		[ "$s" = none ] && continue
		printf '%s\n' "$s" | grep -qx "$SIGNATURE" && continue
		fetch "$forge" "$1" "$SIGNATURE" "$2"
		return
	done
}

publish() {
	t=$1 dry=$2
	[ -z "$SIGNER" ] || signed "$t" || [ "$dry" = 1 ] ||
		die "$t needs no signature until SIGN_AFTER covers it; drop -sign"
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
		# A rehearsal must still reach the signer it was handed, or
		# a signer that cannot sign passes for one that can.
		if [ -n "$SIGNER" ] && [ "$dry" = 1 ]; then
			echo "$t: rehearsing the signer on a scratch file"
			try_sign
		fi
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
	if signed "$t" || [ -n "$SIGNER" ]; then
		seal "$t" "$dir" "$a" "$g" "$dry"
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

# try_sign runs the signer on a scratch file: the rehearsal without the
# build, for checking a signer in seconds.
try_sign() {
	[ -n "$SIGNER" ] || die "-try-sign needs -sign CMD before it"
	dir=$(mktemp -d)
	trap 'rm -rf "$dir"' EXIT
	echo "rehearsal" >"$dir/checksums.txt"
	seal rehearsal "$dir" none none 1
}

dry=0 SIGNER=
while [ $# -gt 0 ]; do
	case $1 in
	-check) check; exit ;;
	-files) shift; for t; do files "$t"; done; exit ;;
	-tidy-sbom) shift; for f; do tidy_sbom "$f"; done; exit ;;
	-try-sign) try_sign; exit ;;
	-n) dry=1 ;;
	-sign) [ $# -gt 1 ] || die "-sign needs a command"; SIGNER=$2; shift ;;
	-*) die "unknown flag $1" ;;
	*) break ;;
	esac
	shift
done
[ $# -gt 0 ] ||
	die "usage: hack/release.sh [-n] [-sign CMD] vX.Y.Z... | -check"
for t; do publish "$t" $dry; done
