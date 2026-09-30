#!/usr/bin/env bash
# Runs a mutant set through scripts/mutate.sh in parallel copies of a checkout.
#
# usage: run_mutants.sh <checkout> <mutants dir> <out dir> [workers, default 4]
#
# MUTATE_TIMEOUT sets the -timeout of every go test run a mutant's mutate.sh
# makes, its baseline, pre-build and verdict run (default 300s): a whole number
# of seconds, minutes or hours, from 1 up to 24 hours, with no leading zero, such
# as 900s or 15m. A baseline the limit cuts short reads RED.
#
# Each <mutants dir>/<id>/ holds file, search and replace, mutate.sh's three
# arguments, each used byte for byte; spell, the TMPDIR spellings to run under
# ("asis", "folded" or both); and optionally pkgs, a package list that replaces
# the computed one. Without pkgs, a mutant runs over the mutated file's package,
# every tracked package whose code or tests import it directly, and ./docs.
#
# ./docs joins every computed set whether or not it imports the target, where the
# checkout has one. It holds the documentation-conformance gates, which judge
# claims about packages it does not import, so an import-graph set alone lets a
# mutation that breaks a documented claim read as survived. Every run sets
# -failfast, -p=2, the -timeout above and MUTATE_BASELINE_CACHE, so a copy runs a
# baseline once per package set, spelling and tree. "folded" is TMPDIR's last component "T" spelled "t", which
# names the same directory only on a case-insensitive volume.
#
# Each worker owns one copy (rsync of the checkout without .claude,
# node_modules, .vscode-test and an out directory inside the checkout) and runs its mutants one at a time; its unstaged tree must be
# clean before and after every run. The checkout's unstaged tree must be clean.
# Writes <out dir>/results.tsv and one log per run under <out dir>/logs.
#
# A run owns its build cache and removes it with the worker trees, the baseline
# stamps and the queues at exit, leaving results.tsv, logs/, pkgsets/,
# work/worker.N.out and work/results.wN.tsv. An interrupted run writes no
# results.tsv, so the files it keeps are its record. Every entry a worker's
# build writes for the checkout's own packages is keyed by the copy's path, so
# a run that kept them would leave a cache nothing reads again: the shared
# cache reached 163 GB in four days of mutant runs and filled the disk.
#
# A mutant that does not build, and a verdict run in which any named package
# did not run, read NOBUILD: the verdict cannot rest on that run. A search that
# matches nothing reads NOMATCH, and a red baseline reads RED. A verdict run in
# which every package that failed ran out of time with no "--- FAIL:" line of its
# own reads TIMEOUT (mutate.sh's exit 3, whose header states that rule), its
# failing-test column naming the tests that were running: run that mutant again,
# alone, with a longer MUTATE_TIMEOUT.
set -euo pipefail
# With CDPATH exported, a `cd` that finds a relative argument through it
# prints the directory it chose, and $(cd "$1" && pwd -P) below would capture
# two lines.
unset CDPATH

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
[ "$out" != "$src" ] || { echo "the out directory $out is the checkout" >&2; exit 2; }
timeout=${MUTATE_TIMEOUT:-300s}
n=${timeout%[smh]}
max=0
case "${timeout#"$n"}" in s) max=86400 ;; m) max=1440 ;; h) max=24 ;; esac
case "$n" in '' | 0* | *[!0-9]*) max=0 ;; esac
if [ "$max" -eq 0 ] || [ "${#n}" -gt 5 ] || [ "$n" -gt "$max" ]; then
	echo "MUTATE_TIMEOUT must be a whole number of seconds, minutes or hours from 1 up to 24 hours, with no leading zero, such as 900s or 15m, got '${MUTATE_TIMEOUT}'" >&2
	exit 2
fi

# An out directory inside the checkout is left out of every worker's copy: a
# later copy would otherwise hold an earlier worker's tree, whose .git a
# mutant's baseline key cannot hash. rsync reads the path as a pattern, and
# reads a backslash as an escape only in a pattern holding a wildcard ([, * or
# ?), so the path is escaped only then; a ] alone is no wildcard. The sed
# reads the path byte-wise, since a file name may hold bytes that are not UTF-8.
copy_excludes=(--exclude '/.claude/' --exclude 'node_modules/' --exclude '.vscode-test/')
case "$out" in
"$src"/*)
	rel=${out#"$src"/}
	case "$rel" in
	*[[*?]*) rel=$(printf '%s' "$rel" | LC_ALL=C sed 's/[][*?\\]/\\&/g') ;;
	esac
	copy_excludes+=(--exclude "/$rel/")
	;;
esac

git -C "$src" diff --quiet || { echo "the checkout's unstaged tree is not clean" >&2; exit 2; }

# The checkout's own toolchain, before any worker starts: the package sets below
# come from `go list` and every verdict comes from scripts/mutate.sh, and the two
# must be one Go. The run works from the checkout, so the go command resolves
# the toolchain there and not in the caller's directory. No path below depends
# on the caller's directory.
cd "$src"
. scripts/toolchain.sh

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

pids=()

# live holds the process groups cleanup signalled that may still have a member.
live=()

# prune_live keeps in live only the groups that still have a member. A group's
# ID is not reused while a member lives, so a group is dropped the first time
# it is found empty and never probed or signalled again.
# shellcheck disable=SC2329 # cleanup, which the EXIT trap invokes, calls it
prune_live() {
	local g
	local kept=()
	for g in ${live[@]+"${live[@]}"}; do
		if kill -0 -- "-$g" 2>/dev/null; then kept+=("$g"); fi
	done
	live=(${kept[@]+"${kept[@]}"})
}

# The workers stop before the removal: one that still builds writes back what
# the removal takes. Each worker leads its own process group, so one signal
# reaches every process it started that stays in that group; a worker's go
# test otherwise outlives it (bash 5) or holds the worker's own exit until it
# returns (bash 3.2). A run of this script inside a worker starts groups of its
# own, which only that run's cleanup reaches. A group still running after four
# to five seconds (SECONDS counts whole seconds) is killed. Only a job bash
# still lists as running is signalled: bash reaps a finished worker before any
# `wait` names it, and the system may then give its PID to another process. A
# second INT or TERM is ignored, so it cannot cut the stop or the removal short.
# shellcheck disable=SC2329 # the EXIT trap below is what invokes it
cleanup() {
	local p d end
	trap '' INT TERM
	for p in $(jobs -pr); do
		kill -TERM -- "-$p" 2>/dev/null || true
		live+=("$p")
	done
	prune_live
	end=$((SECONDS + 5))
	while [ "${#live[@]}" -gt 0 ] && [ "$SECONDS" -lt "$end" ]; do
		sleep 0.1
		prune_live
	done
	for p in ${live[@]+"${live[@]}"}; do
		kill -KILL -- "-$p" 2>/dev/null || true
	done
	while [ "${#live[@]}" -gt 0 ]; do
		sleep 0.1
		prune_live
	done
	wait 2>/dev/null || true
	rm -rf -- "$out/work/gocache"
	for d in "$out"/work/w[0-9]*/ "$out"/work/baselines.w[0-9]*/; do
		[ -d "$d" ] || continue
		rm -rf -- "$d"
	done
	rm -f -- "$out"/work/queue.*
}
trap cleanup EXIT
# Without these the exit trap never runs on a signal, which is how a cancelled
# run left its trees behind.
trap 'exit 130' INT
trap 'exit 143' TERM

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

# readbytes <var> <file> assigns the file's bytes to var, trailing newlines
# included. Command substitution would strip them, and a search that loses its
# newline can match more sites than the one it names.
readbytes() {
	local v
	v=$(cat "$2"; printf x)
	printf -v "$1" '%s' "${v%x}"
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
	# Refused here, before any copy: the name is used byte for byte, and a
	# trailing newline names a file the checkout does not hold.
	readbytes file "$d/file"
	[ -f "$src/$file" ] || { printf 'mutant %s names %q, which the checkout does not hold\n' "$id" "$file" >&2; exit 2; }
	if [ ! -f "$d/pkgs" ]; then
		pkgset "$file" >/dev/null
	fi
	echo "$id" >>"$out/work/queue.$((i % workers + 1))"
	i=$((i + 1))
done
[ "$i" -gt 0 ] || { echo "no mutants in $mutants" >&2; exit 2; }
echo "mutants: $i, workers: $workers"

for w in $(seq 1 "$workers"); do
	[ -f "$out/work/queue.$w" ] || continue
	rsync -a --delete "${copy_excludes[@]}" "$src/" "$out/work/w$w/"
done

worker() {
	local w=$1 tree="$out/work/w$1" id d file search replace pkgs spell tdir log start rc secs verdict fails output line
	while IFS= read -r id <&3; do
		d="$mutants/$id"
		readbytes file "$d/file"
		readbytes search "$d/search"
		readbytes replace "$d/replace"
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
			output=$(cd "$tree" && TMPDIR="$tdir" GOFLAGS="-failfast=true -timeout=$timeout -p=2" \
				GOCACHE="$out/work/gocache" \
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
			# mutate.sh exits 0 on a kill and 3 on a timeout; the other verdicts share
			# exit 1, and its own first verdict line, before any test output, splits them.
			line=$(printf '%s\n' "$output" | LC_ALL=C grep -m 1 -E '^mutate: (MUTANT |NO TEST RAN|the mutated tree DOES NOT BUILD|the UNMUTATED tree|the search string matched NOTHING|the replacement left )' || true)
			[ "$rc" -ne 0 ] || line="mutate: MUTANT KILLED"
			[ "$rc" -ne 3 ] || line="mutate: MUTANT TIMED OUT"
			case "$line" in
			"mutate: MUTANT TIMED OUT"*) verdict=TIMEOUT ;;
			"mutate: MUTANT KILLED"*) verdict=KILLED ;;
			"mutate: MUTANT SURVIVED"*) verdict=SURVIVED ;;
			"mutate: the mutated tree DOES NOT BUILD"* | "mutate: NO TEST RAN"*) verdict=NOBUILD ;;
			"mutate: the UNMUTATED tree"*) verdict=RED ;;
			"mutate: the search string matched NOTHING"* | "mutate: the replacement left "*) verdict=NOMATCH ;;
			*) verdict="OTHER(rc=$rc)" ;;
			esac
			# Byte-wise: a test's output may hold bytes that are not UTF-8,
			# which macOS sed and tr refuse under a UTF-8 locale, stopping the
			# worker.
			if [ "$verdict" = TIMEOUT ]; then
				fails=$(printf '%s\n' "$output" | LC_ALL=C awk '/^\trunning tests:$/ { r = 1; next } r && /^\t\t[^ \t]+ \(/ { sub(/^\t\t/, ""); sub(/ \(.*/, ""); print; next } { r = 0 }' | LC_ALL=C sort -u | LC_ALL=C tr '\n' ' ')
			else
				fails=$(printf '%s\n' "$output" | LC_ALL=C sed -nE 's/^ *--- FAIL: ([^ ]+).*/\1/p' | LC_ALL=C sort -u | LC_ALL=C tr '\n' ' ')
			fi
			printf '%s\t%s\t%s\t%s\tw%s\t%s\t%s\n' "$id" "$spell" "$verdict" "$secs" "$w" "$fails" \
				"$(printf '%s' "$pkgs" | tr '\n' ' ')" >>"$out/work/results.w$w.tsv"
			echo "$id $spell $verdict ${secs}s"
			git -C "$tree" diff --quiet || { echo "w$w: unstaged tree dirty after $id/$spell" >&2; return 3; }
		done
	done 3<"$out/work/queue.$w"
}

began=$(date +%s)
# Job control gives each worker the process group cleanup signals.
set -m
for w in $(seq 1 "$workers"); do
	[ -f "$out/work/queue.$w" ] || continue
	worker "$w" >"$out/work/worker.$w.out" 2>&1 &
	pids+=("$!")
done
set +m
status=0
for p in "${pids[@]}"; do
	wait "$p" || status=1
done

# A worker that stopped before its first verdict wrote no results file; the
# summary still runs, so the run reports it.
shopt -s nullglob
parts=("$out"/work/results.w*.tsv)
shopt -u nullglob
{
	printf 'id\tspelling\tverdict\tseconds\tworker\tfailing tests\tpackages\n'
	if [ "${#parts[@]}" -gt 0 ]; then LC_ALL=C sort "${parts[@]}"; fi
} >"$out/results.tsv"
echo "wall clock: $(($(date +%s) - began))s"
awk -F'\t' 'NR > 1 { n[$3]++; s += $4 } END { for (v in n) printf "  %s: %d\n", v, n[v]; printf "  run seconds, summed: %d\n", s }' "$out/results.tsv"
if [ "$status" -ne 0 ]; then
	echo "a worker stopped early; see $out/work/worker.*.out" >&2
fi
exit "$status"
