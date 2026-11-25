#!/bin/sh
# sign-ssh.sh signs releases with Rastrillo's release key (discussion
# #417): an ed25519 SSH key that 1Password generated and holds in the
# "Rastrillo Release" vault. hack/allowed_signers names its public half.
#
#   hack/sign-ssh.sh sign FILE SIG     sign FILE, writing SIG
#   hack/sign-ssh.sh verify FILE SIG   check SIG against allowed_signers
#   hack/sign-ssh.sh tag TAG SHA       make TAG an SSH-signed annotated tag
#   hack/sign-ssh.sh verify-tag TAG    check TAG was signed by the release key
#
# sign and verify are the CMD that `hack/release.sh -sign` runs.
#
# The private key never touches disk. It goes from `op read` straight
# into an ssh-agent started for this run alone, with a five-minute
# lifetime, and the agent is killed on exit. op reads it with a service
# account that can see only that vault. Its token sits in the file
# RELEASE_OP_TOKEN names, readable by its owner alone.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
signers=$here/allowed_signers
identity=release@rastrillo.org
item="op://Rastrillo Release/Rastrillo release signing key"
token=${RELEASE_OP_TOKEN:-$HOME/.config/agent-secrets/rastrillo-release.op-token}

die() { echo "sign-ssh: $*" >&2; exit 1; }

public= ours=
# cleanup kills only the agent this run started, which ours names once
# load has one. An inherited SSH_AGENT_PID is never touched.
cleanup() {
	[ -z "$ours" ] || SSH_AGENT_PID=$ours ssh-agent -k >/dev/null 2>&1 || true
	[ -z "$public" ] || rm -f "$public"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

# load starts a private agent and adds the key to it. The caller's own
# agent is set aside first, so a failed start cannot leave the key in
# it, and cleanup cannot kill it.
load() {
	[ -r "$token" ] || die "no 1Password token at $token"
	unset SSH_AUTH_SOCK SSH_AGENT_PID
	agent=$(ssh-agent -s -t 300) || die "could not start ssh-agent"
	eval "$agent" >/dev/null
	ours=${SSH_AGENT_PID:-}
	[ -n "$ours" ] && [ -S "${SSH_AUTH_SOCK:-}" ] ||
		die "ssh-agent started without a socket"
	OP_SERVICE_ACCOUNT_TOKEN=$(cat "$token") \
		op read "$item/private key?ssh-format=openssh" |
		ssh-add -q -t 300 - || die "could not load the release key"
	public=$(mktemp)
	cut -d' ' -f3- "$signers" >"$public"
}

# verify_tag checks that tag carries an SSH signature by the release
# identity in the git namespace. git verify-tag alone would also accept
# a GPG signature the local keyring trusts, which is not the release key.
verify_tag() {
	object=$(git cat-file tag "$1") || die "no tag $1"
	signature='-----BEGIN SSH SIGNATURE-----'
	case $object in
	*"$signature"*) ;;
	*) die "$1 has no SSH signature" ;;
	esac
	scratch=$(mktemp -d)
	git cat-file tag "$1" | sed "/^$signature\$/,\$d" >"$scratch/payload"
	git cat-file tag "$1" | sed -n "/^$signature\$/,\$p" >"$scratch/sig"
	ssh-keygen -q -Y verify -f "$signers" -I "$identity" -n git \
		-s "$scratch/sig" <"$scratch/payload" >/dev/null
	ok=$?
	rm -rf "$scratch"
	return $ok
}

case ${1:-} in
sign)
	[ $# = 3 ] || die "usage: sign FILE SIG"
	load
	ssh-keygen -q -Y sign -n file -f "$public" "$2" ||
		die "ssh-keygen could not sign $2"
	[ "$2.sig" = "$3" ] || mv "$2.sig" "$3"
	;;
verify)
	[ $# = 3 ] || die "usage: verify FILE SIG"
	ssh-keygen -q -Y verify -f "$signers" -I "$identity" -n file \
		-s "$3" <"$2" >/dev/null
	;;
tag)
	[ $# = 3 ] || die "usage: tag TAG SHA"
	load
	git -c user.name=claude -c user.email=claude~paul@keymail.dev \
		-c gpg.format=ssh -c "user.signingkey=key::$(cat "$public")" \
		tag -s -m "$2" "$2" "$3"
	verify_tag "$2" ||
		die "$2 does not verify; delete it with git tag -d $2"
	;;
verify-tag)
	[ $# = 2 ] || die "usage: verify-tag TAG"
	verify_tag "$2" || die "$2 is not signed by the release key"
	;;
*)
	die "usage: sign FILE SIG | verify FILE SIG | tag TAG SHA | verify-tag TAG"
	;;
esac
