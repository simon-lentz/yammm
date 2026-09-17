#!/usr/bin/env bash
# Runs a mutant set through scripts/mutate.sh in parallel copies of a checkout.
#
# usage: run_mutants.sh <checkout> <mutants dir> <out dir> [workers, default 4]
#
# Each <mutants dir>/<id>/ holds file, search and replace, mutate.sh's three
# arguments, each used byte for byte; spell, the TMPDIR spellings to run under
# ("asis", "folded" or both); and optionally pkgs, a package list that replaces
# the computed one. Without pkgs, a mutant runs over the mutated file's package,
# every tracked package whose code or tests import it directly, and ./docs.
#
# ./docs joins every computed set whether or not it imports the target, where the
# checkout has one. It holds the documentation-conformance gates and imports 12
# of this module's 54 packages, so an import-graph set alone lets a mutation that
# breaks a documented claim read as survived. Every run sets
# -failfast, -p=2 and MUTATE_BASELINE_CACHE, so a copy runs a baseline once per package set,
# spelling and tree. "folded" is TMPDIR's last component "T" spelled "t", which
# names the same directory only on a case-insensitive volume.
#
# Each worker owns one copy (rsync of the checkout without .claude and
# node_modules) and runs its mutants one at a time; its unstaged tree must be
# clean before and after every run. The checkout's unstaged tree must be clean.
# Writes <out dir>/results.tsv and one log per run under <out dir>/logs.
set -euo pipefail

usage() {
	echo "usage: $0 <checkout> <mutants dir> <out dir> [workers]" >&2
	exit 2
}

[ $# -ge 3 ] && [ $# -le 4 ] || usage
src=$(cd "$1" && pwd -P)
mutants=$(cd "$2" && pwd -P)
mkdir -p "$3"
out=$(cd "$3" && pwd -P)
workers=${4:-4}
case "$workers" in
'' | *[!0-9]* | 0) echo "workers must be a positive integer, got '$workers'" >&2; exit 2 ;;
esac

git -C "$src" diff --quiet || { echo "the checkout's unstaged tree is not clean" >&2; exit 2; }

real_t=$(cd "${TMPDIR:-/tmp}" && pwd -P)
folded_t=""
case "$real_t" in
*/T)
	candidate="${real_t%/T}/t"
	# Probe by file, not by inode: stat's inode flag is -f on BSD, -c on GNU.
	if [ -d "$candidate" ]; then
		probe="$real_t/.foldprobe.$$"
		: >"$probe"
		if [ -e "$candidate/.foldprobe.$$" ]; then
			folded_t=$candidate
		fi
		rm -f "$probe"
	fi
	;;
esac

mkdir -p "$out/logs" "$out/work" "$out/pkgsets"
rm -f "$out"/work/queue.* "$out"/work/results.w*.tsv "$out"/work/worker.*.out

module=$(cd "$src" && go list -m)
tracked=$(cd "$src" && bash scripts/packages.sh 2>/dev/null)
# shellcheck disable=SC2086 # tracked is a word list of import paths
(cd "$src" && go list -f '{{.ImportPath}}{{"\t"}}{{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' $tracked) >"$out/pkgsets/imports.tsv"

docs_pkg=""
if cut -f1 "$out/pkgsets/imports.tsv" | grep -qx "$module/docs"; then
	docs_pkg="./docs"
fi

# pkgset prints the ./ paths of the package holding file, of every tracked
# package whose code or tests import it directly, and of docs_pkg.
pkgset() {
	local target key
	target=$(cd "$src" && go list -f '{{.ImportPath}}' "./$(dirname "$1")")
	key=$(printf '%s' "$target" | tr '/' '_')
	if [ ! -f "$out/pkgsets/$key" ]; then
		{
			awk -F'\t' -v t="$target" -v m="$module" '
				{
					hit = ($1 == t)
					n = split($2, imports, " ")
					for (i = 1; i <= n && !hit; i++) if (imports[i] == t) hit = 1
					if (hit) print ($1 == m) ? "." : "./" substr($1, length(m) + 2)
				}' "$out/pkgsets/imports.tsv"
			if [ -n "$docs_pkg" ]; then echo "$docs_pkg"; fi
		} | LC_ALL=C sort -u >"$out/pkgsets/$key"
	fi
	cat "$out/pkgsets/$key"
}

readbytes() {
	local v
	v=$(cat "$1"; printf x)
	printf '%s' "${v%x}"
}

i=0
for d in "$mutants"/*/; do
	# An empty directory leaves the pattern unexpanded, and basename would
	# then report a mutant named "*".
	[ -d "$d" ] || continue
	id=$(basename "$d")
	for f in file search replace spell; do
		[ -f "$d/$f" ] || { echo "mutant $id has no $f file" >&2; exit 2; }
	done
	if [ ! -f "$d/pkgs" ]; then
		pkgset "$(readbytes "$d/file")" >/dev/null
	fi
	echo "$id" >>"$out/work/queue.$((i % workers + 1))"
	i=$((i + 1))
done
[ "$i" -gt 0 ] || { echo "no mutants in $mutants" >&2; exit 2; }
echo "mutants: $i, workers: $workers"

for w in $(seq 1 "$workers"); do
	[ -f "$out/work/queue.$w" ] || continue
	rsync -a --delete --exclude '/.claude/' --exclude 'node_modules/' --exclude '.vscode-test/' "$src/" "$out/work/w$w/"
done

worker() {
	local w=$1 tree="$out/work/w$1" id d file search replace pkgs spell tdir log start rc secs verdict fails output
	while IFS= read -r id <&3; do
		d="$mutants/$id"
		file=$(readbytes "$d/file")
		search=$(readbytes "$d/search")
		replace=$(readbytes "$d/replace")
		if [ -f "$d/pkgs" ]; then pkgs=$(cat "$d/pkgs"); else pkgs=$(pkgset "$file"); fi
		spells=$(cat "$d/spell")
		# shellcheck disable=SC2086 # spells is a word list
		for spell in $spells; do
			case "$spell" in
			asis) tdir=$real_t/ ;;
			folded)
				if [ -z "$folded_t" ]; then
					printf '%s\t%s\tNOFOLD\t0\tw%s\t\t\n' "$id" "$spell" "$w" >>"$out/work/results.w$w.tsv"
					continue
				fi
				tdir=$folded_t/
				;;
			*) echo "w$w: mutant $id names an unknown spelling '$spell'" >&2; return 2 ;;
			esac
			git -C "$tree" diff --quiet || { echo "w$w: unstaged tree dirty before $id/$spell" >&2; return 3; }
			log="$out/logs/$id.$spell.log"
			start=$(date +%s)
			set +e
			# shellcheck disable=SC2086 # pkgs is a word list
			output=$(cd "$tree" && TMPDIR="$tdir" GOFLAGS="-failfast=true -timeout=300s -p=2" \
				MUTATE_BASELINE_CACHE="$out/work/baselines.w$w" \
				bash scripts/mutate.sh "$file" "$search" "$replace" $pkgs 2>&1 </dev/null)
			rc=$?
			set -e
			secs=$(($(date +%s) - start))
			{
				echo "id=$id spell=$spell worker=$w TMPDIR=$tdir rc=$rc secs=$secs"
				echo "pkgs=$(printf '%s' "$pkgs" | tr '\n' ' ')"
				echo "$output"
			} >"$log"
			case "$output" in
			*"MUTANT KILLED"*) verdict=KILLED ;;
			*"MUTANT SURVIVED"*) verdict=SURVIVED ;;
			*"DOES NOT BUILD"*) verdict=NOBUILD ;;
			*"already red"*) verdict=RED ;;
			*"matched NOTHING"* | *"did not apply"*) verdict=NOMATCH ;;
			*) verdict="OTHER(rc=$rc)" ;;
			esac
			fails=$(printf '%s\n' "$output" | sed -nE 's/^ *--- FAIL: ([^ ]+).*/\1/p' | LC_ALL=C sort -u | tr '\n' ' ')
			printf '%s\t%s\t%s\t%s\tw%s\t%s\t%s\n' "$id" "$spell" "$verdict" "$secs" "$w" "$fails" \
				"$(printf '%s' "$pkgs" | tr '\n' ' ')" >>"$out/work/results.w$w.tsv"
			echo "$id $spell $verdict ${secs}s"
			git -C "$tree" diff --quiet || { echo "w$w: unstaged tree dirty after $id/$spell" >&2; return 3; }
		done
	done 3<"$out/work/queue.$w"
}

began=$(date +%s)
pids=()
for w in $(seq 1 "$workers"); do
	[ -f "$out/work/queue.$w" ] || continue
	worker "$w" >"$out/work/worker.$w.out" 2>&1 &
	pids+=("$!")
done
status=0
for p in "${pids[@]}"; do
	wait "$p" || status=1
done

{
	printf 'id\tspelling\tverdict\tseconds\tworker\tfailing tests\tpackages\n'
	cat "$out"/work/results.w*.tsv 2>/dev/null | LC_ALL=C sort
} >"$out/results.tsv"
echo "wall clock: $(($(date +%s) - began))s"
awk -F'\t' 'NR > 1 { n[$3]++; s += $4 } END { for (v in n) printf "  %s: %d\n", v, n[v]; printf "  run seconds, summed: %d\n", s }' "$out/results.tsv"
if [ "$status" -ne 0 ]; then
	echo "a worker stopped early; see $out/work/worker.*.out" >&2
fi
exit "$status"
