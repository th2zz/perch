package records

// One more way to learn which session a pane is running, for the panes that never said: codex
// 0.163's managed app-server keeps the environment of whichever pane first started it, so a
// fresh pane's SessionStart hook can report its session to the wrong pane — or to a pane that no
// longer exists — and herdr's mirror stays empty. The daemon, not the pane's process tree, holds
// the rollout open, so the kernel cannot name it either. What remains is what codex itself wrote
// down: a `threads` row in its state database, anchored to the moment this pane's codex launched.
//
// This is the weakest of the sources and is consulted only when none of the others found
// anything, so it can never overwrite a name that came from the harness. Where a launch and a
// row cannot be paired uniquely it refuses instead of guessing: a phone showing the wrong
// conversation is worse than one showing the terminal.
//
// The row born with the launch is not the whole answer, because codex's `/new` keeps the process
// running and writes a row anchored to the /new moment — a row no launch explains — and the pane
// may already be living in that newer session. So after a row is matched, every newer row in the
// same directory must be accounted for by a live codex launch (found by walking /proc
// machine-wide: the launch that explains a colleague's row is under a different pane, never
// under ours). A newer row nobody explains means the match may already be history, and the
// source refuses. The accepted costs, all paid to never show the wrong conversation: after
// `/new` the pane refuses until it is relaunched; a colleague's pane that opened and closed in
// the same directory leaves rows nobody explains, so the directory's other panes refuse until
// they are relaunched; two launches whose rows land within the winner's lead of each other
// poison each other's panes.
//
// Measured on this machine (2026-10-17): the row's creation time lands 875–1040 ms after the
// codex process starts (four launches, including two started 7 ms apart), and the row reaches
// the database only when the session first does something — the first prompt — while its
// creation time stays anchored to the launch. That last fact is what makes the window stand:
// it is stated once, absolutely, and never needs refreshing while the pane runs.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A codex launch, as the kernel saw it: when the codex process in the pane started, and the
// directory it started in. The session's row must have been born after the launch, in that same
// directory.
type codexWindow struct {
	startMS int64
	cwd     string
}

// One `threads` row of codex's state database: the identity, the record file, the directory the
// session lives in, and the moment codex created the session.
type stateThread struct {
	id        string
	path      string
	createdMS int64
	updatedMS int64
	cwd       string
}

// The numbers that make the state database answer honestly.
//
// The floor sits well below the measured 875 ms (a faster machine may launch a session sooner;
// an earlier launch's row, which a slower machine's launch could otherwise adopt, sits closer to
// the earlier launch than the floor allows). The ceiling leaves room for a cold app-server spawn.
// The lead is what separates "one launch, one row" from "two launches in the same breath": when
// another row sits that close behind the winner, the moment cannot tell the two launches apart.
const (
	statePreLaunchMS   = 30_000
	stateWindowFloorMS = 300
	stateWindowCeilMS  = 180_000
	stateWinnerLeadMS  = 1200
)

// stateScanLimit caps every question that reads a span of rows — the candidates around this
// pane's launches and the rows inside one launch's window. A result that fills the cap is not an
// answer: whole rows that a decision would rest on could be the ones the cap left out, so a full
// result is refused rather than trusted. Every span asked about is bounded by construction (a
// window is three minutes wide), so the cap never grows with how much history a directory has.
const stateScanLimit = 64

// codexStateRollout finds a pane's rollout through codex's own state database.
//
// It is the last resort: a pane reaches it only after herdr's registration, the command line and
// the kernel's open files all came up empty. What it answers with has passed every guard: a row
// whose launch directory is the pane's own, whose creation time follows that launch by a
// believable margin and leads every other candidate clearly (pickStateThread), with no newer row
// in the same directory left unexplained (see the file comment), whose record file exists on
// disk, and whose file name carries the row's own id — the row's word is this pane's session
// only when codex's file agrees with it.
//
// Its questions run under one short budget (see sqliteQueryBudget): this sits on the phone's
// polling path and the database is shared with a live codex.
func codexStateRollout(home string, w Where) (path, id string) {
	windows := codexStartWindows(w.PID)
	if len(windows) == 0 {
		return "", ""
	}
	db := newestCodexStateDB(home)
	tool := sqliteTool()
	if db == "" || tool == "" {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), sqliteQueryBudget)
	defer cancel()
	out, err := querySQLite(ctx, tool, db, stateThreadsQuery(windows))
	if err != nil {
		return "", ""
	}
	rows := parseStateThreads(out)
	if len(rows) >= stateScanLimit {
		return "", ""
	}
	thread, ok := pickStateThread(rows, windows)
	if !ok {
		if len(rows) == 0 {
			return codexResumedStateRollout(ctx, tool, db, windows, w.Title)
		}
		return "", ""
	}
	// The matched row can be history: a /new wrote a newer row with no launch behind it, and a
	// colleague's pane wrote its own rows. Every newer row in the same directory must be
	// explained by a launch that is alive right now — a machine-wide walk, because that launch
	// need not be anywhere under this pane — or this pane's session is no longer provable. The
	// questions are one per live launch (the rows inside its window) plus one existence question
	// for whatever no window covers; asking for the directory's whole newer history instead
	// would tie serving to how busy the directory has been, and every long-lived pane would end
	// up refusing on history alone.
	launches := codexLaunchesIn(thread.cwd)
	relevant := make([]codexWindow, 0, len(launches))
	for _, launch := range launches {
		// A window that closed before the matched row was born can hold no successor; asking
		// about spans a decision cannot use is how a full result would refuse for nothing.
		if launch.startMS+stateWindowCeilMS > thread.createdMS {
			relevant = append(relevant, launch)
		}
	}
	windowRows := make([][]stateThread, 0, len(relevant))
	for _, launch := range relevant {
		out, err = querySQLite(ctx, tool, db, stateWindowThreadsQuery(launch))
		if err != nil {
			return "", ""
		}
		window := parseStateThreads(out)
		if len(window) >= stateScanLimit {
			return "", ""
		}
		windowRows = append(windowRows, window)
	}
	preLaunchStart := int64(0)
	for _, launch := range windows {
		if launch.startMS > thread.createdMS &&
			(preLaunchStart == 0 || launch.startMS < preLaunchStart) {
			preLaunchStart = launch.startMS
		}
	}
	out, err = querySQLite(ctx, tool, db, stateUncoveredThreadsQuery(thread, relevant, preLaunchStart))
	if err != nil {
		return "", ""
	}
	if unexplainedSuccessor(thread, relevant, windowRows, parseStateThreads(out)) {
		return "", ""
	}
	if _, err := os.Stat(thread.path); err != nil {
		return "", ""
	}
	if sessionIDFromRecordName(thread.path) != thread.id {
		return "", ""
	}
	return thread.path, thread.id
}

// codexResumedStateRollout recognizes a pane that restarted codex on an existing session.
//
// The launch window says "the row was born with this process", which is false after a restart:
// the row predates the launch by hours while the managed app-server keeps updating it. Such a
// row is accepted only when it is the single old row in the pane's directory touched after the
// launch, no row was born around this launch, its record was written after the launch, and its
// name is the pane's own terminal title. Those guards make it a resume of this pane's session,
// not a guess from a directory's history.
func codexResumedStateRollout(
	ctx context.Context,
	tool, db string,
	windows []codexWindow,
	title string,
) (string, string) {
	launch := int64(0)
	for _, w := range windows {
		if w.startMS > launch {
			launch = w.startMS
		}
	}
	if launch == 0 {
		return "", ""
	}
	out, err := querySQLite(ctx, tool, db, stateResumedThreadsQuery(windows, launch))
	if err != nil {
		return "", ""
	}
	rows := parseResumedThreads(out)
	if len(rows) != 1 {
		return "", ""
	}
	row := rows[0]
	if row.updatedMS < launch || !titleMatchesThread(title, row) {
		return "", ""
	}
	out, err = querySQLite(ctx, tool, db, stateNewRowQuery(windows[0].cwd, launch))
	if err != nil || len(parseStateThreads(out)) > 0 {
		return "", ""
	}
	info, err := os.Stat(row.path)
	if err != nil || info.ModTime().UnixMilli() < launch {
		return "", ""
	}
	if sessionIDFromRecordName(row.path) != row.id {
		return "", ""
	}
	return row.path, row.id
}

type resumedThread struct {
	id, path, cwd, name, title string
	createdMS, updatedMS       int64
}

func titleMatchesThread(title string, row resumedThread) bool {
	pane := strings.ToLower(strings.TrimSpace(title))
	for _, candidate := range []string{row.name, row.title} {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate != "" && strings.Contains(pane, candidate) {
			return true
		}
	}
	return false
}

func parseResumedThreads(out []byte) []resumedThread {
	var threads []resumedThread
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 || fields[0] == "" || fields[1] == "" || fields[3] == "" {
			continue
		}
		created, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		updated, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			continue
		}
		threads = append(threads, resumedThread{
			id: fields[0], path: fields[1], createdMS: created, cwd: fields[3],
			updatedMS: updated, name: fields[5], title: fields[6],
		})
	}
	return threads
}

func stateResumedThreadsQuery(windows []codexWindow, launch int64) string {
	return fmt.Sprintf(
		"select id || char(9) || rollout_path || char(9) || created_at_ms || char(9) || cwd || "+
			"char(9) || coalesce(updated_at_ms, created_at_ms) || char(9) || coalesce(name, '') || "+
			"char(9) || coalesce(title, '') from threads "+
			"where originator = 'codex-tui' and cwd = '%s' and created_at_ms < %d "+
			"and updated_at_ms >= %d order by updated_at_ms desc limit 2",
		strings.ReplaceAll(windows[0].cwd, "'", "''"),
		launch-statePreLaunchMS, launch,
	)
}

func stateNewRowQuery(cwd string, launch int64) string {
	return fmt.Sprintf(
		"select id || char(9) || rollout_path || char(9) || created_at_ms || char(9) || cwd || "+
			"char(9) || coalesce(updated_at_ms, created_at_ms) from threads "+
			"where originator = 'codex-tui' and cwd = '%s' and created_at_ms >= %d limit 1",
		strings.ReplaceAll(cwd, "'", "''"), launch-statePreLaunchMS,
	)
}

// codexStartWindows lists the codex launches visible under one pane pid.
//
// The pane's foreground process is not the codex binary (a node wrapper sits in front), so the
// whole tree is walked — the same walk the kernel source uses. A process counts as codex when
// its own binary is named `codex` or when it names one as its command: the wrapper's first
// argument is the codex script, and a plain `codex` from PATH is the binary itself.
func codexStartWindows(pid int) []codexWindow {
	var windows []codexWindow
	for _, p := range processTree(pid) {
		if !isCodexProcess(p) {
			continue
		}
		start := processStartMS(p)
		cwd := processCWD(p)
		if start == 0 || cwd == "" {
			continue
		}
		windows = append(windows, codexWindow{startMS: start, cwd: cwd})
	}
	return windows
}

// processTree lists a process and its descendants, breadth first.
//
// The tree, not just the process, is the point: an agent's foreground process is often a wrapper
// around the one that matters. The walk is bounded because it runs on every phone poll while a
// pane can be running a large build with thousands of processes — de facto chains are a handful
// deep and narrow.
func processTree(pid int) []int {
	if pid <= 0 {
		return nil
	}
	seen := map[int]bool{pid: true}
	tree := []int{pid}
	queue := []int{pid}
	const maxWalked = 128
	for depth := 0; len(queue) > 0 && depth < 8 && len(tree) < maxWalked; depth++ {
		var next []int
		for _, p := range queue {
			for _, child := range processChildren(p) {
				if len(tree) >= maxWalked {
					break
				}
				if !seen[child] {
					seen[child] = true
					tree = append(tree, child)
					next = append(next, child)
				}
			}
		}
		queue = next
	}
	return tree
}

// isCodexProcess reports whether one process is a codex launch, by the two shapes this machine's
// panes present it in: the real binary is named `codex` whatever its argv says, and the wrapper
// in front names the codex script as its own command, which lives in argv[0] or argv[1]. A codex
// path further down a command line is somebody's argument (`git diff /bin/codex`, `npm test --
// /bin/codex`), not a launch, and must not license a window.
func isCodexProcess(pid int) bool {
	proc := filepath.Join("/proc", strconv.Itoa(pid))
	if exe, err := os.Readlink(filepath.Join(proc, "exe")); err == nil && filepath.Base(exe) == "codex" {
		return true
	}
	cmdline, err := os.ReadFile(filepath.Join(proc, "cmdline"))
	if err != nil {
		return false
	}
	args := strings.Split(string(cmdline), "\x00")
	for i := 0; i < len(args) && i < 2; i++ {
		if filepath.Base(args[i]) == "codex" {
			return true
		}
	}
	return false
}

// processStartMS is when a process was launched, in epoch milliseconds.
//
// /proc states the launch as ticks since boot, and the kernel fixes those at 100 per second for
// /proc whatever its own clock runs at, so ticks × 10 is milliseconds. The boot instant itself
// is the subtle part: /proc/stat's btime is a whole second, and losing its fraction would blur
// the floor this window stands on by up to a second — enough to let an earlier launch's row in.
// /proc/uptime carries hundredths, so the subtraction stays within a few tens of milliseconds.
func processStartMS(pid int) int64 {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0
	}
	// The comm field can hold any bytes between its parentheses, so the fields after it are
	// counted from the last ")". starttime is field 22 overall, the 20th after the state.
	cut := strings.LastIndexByte(string(stat), ')')
	if cut < 0 {
		return 0
	}
	fields := strings.Fields(string(stat[cut+1:]))
	if len(fields) < 20 {
		return 0
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0
	}
	uptime, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	uptimeFields := strings.Fields(string(uptime))
	if len(uptimeFields) == 0 {
		return 0
	}
	seconds, err := strconv.ParseFloat(uptimeFields[0], 64)
	if err != nil {
		return 0
	}
	return time.Now().UnixMilli() - int64(seconds*1000) + ticks*10
}

// processCWD is a process's working directory, as the kernel sees it.
func processCWD(pid int) string {
	target, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	if err != nil {
		return ""
	}
	return target
}

// codexLaunchesIn lists every live codex launch whose working directory is one path — whether or
// not it descends from the pane that asked.
//
// The walk is machine-wide because the launches this is asked about are other panes': the
// colleague's row in the same directory was written by a process that never appears under our
// pid, and /proc is the only registry of those. The directory test comes first so the cost on
// the polling path is one readlink per process plus a command-line read only for the few that
// run in the directory in question.
func codexLaunchesIn(cwd string) []codexWindow {
	if cwd == "" {
		return nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var launches []codexWindow
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if processCWD(pid) != cwd {
			continue
		}
		if !isCodexProcess(pid) {
			continue
		}
		if start := processStartMS(pid); start != 0 {
			launches = append(launches, codexWindow{startMS: start, cwd: cwd})
		}
	}
	return launches
}

// newestCodexStateDB finds the newest codex state database under a home. The name carries its
// schema generation (`state_5.sqlite`); the highest wins, and an upgrade that bumps the number
// keeps being found. The -wal and -shm companions of a live database are not databases.
func newestCodexStateDB(home string) string {
	matches, err := filepath.Glob(filepath.Join(home, ".codex", "state_*.sqlite"))
	if err != nil {
		return ""
	}
	best, bestVersion := "", -1
	for _, match := range matches {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(match), "state_"), ".sqlite")
		version, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		if version > bestVersion {
			best, bestVersion = match, version
		}
	}
	return best
}

// stateThreadsQuery builds the first of the two questions the state database is ever asked: the
// interactive sessions born around any of this pane's launches, in the launch's own directory.
//
// The cwd is quoted by doubling its quotes — the same rule the goal lookup follows — so nothing
// read out of /proc can end the literal. Columns are glued with char(9) rather than a
// configurable separator argument, so the output parses by the one rule parseStateThreads knows.
func stateThreadsQuery(windows []codexWindow) string {
	parts := make([]string, 0, len(windows))
	for _, w := range windows {
		parts = append(parts, fmt.Sprintf(
			"(cwd = '%s' and created_at_ms between %d and %d and "+
				"(created_at_ms >= %d or updated_at_ms >= %d))",
			strings.ReplaceAll(w.cwd, "'", "''"),
			w.startMS-statePreLaunchMS, w.startMS+stateWindowCeilMS,
			w.startMS+stateWindowFloorMS, w.startMS))
	}
	return "select id || char(9) || rollout_path || char(9) || created_at_ms || char(9) || cwd || " +
		"char(9) || coalesce(updated_at_ms, created_at_ms) from threads " +
		"where originator = 'codex-tui' and (" + strings.Join(parts, " or ") + ") " +
		"order by created_at_ms limit " + strconv.Itoa(stateScanLimit)
}

// stateWindowThreadsQuery asks for the rows inside one live launch's window: every session born
// while that launch was starting is a candidate for its own, and their order decides which row
// the launch gets to explain.
func stateWindowThreadsQuery(launch codexWindow) string {
	return fmt.Sprintf(
		"select id || char(9) || rollout_path || char(9) || created_at_ms || char(9) || cwd || "+
			"char(9) || coalesce(updated_at_ms, created_at_ms) from threads "+
			"where originator = 'codex-tui' and cwd = '%s' and created_at_ms between %d and %d "+
			"and (created_at_ms >= %d or updated_at_ms >= %d) "+
			"order by created_at_ms limit %d",
		strings.ReplaceAll(launch.cwd, "'", "''"),
		launch.startMS-statePreLaunchMS, launch.startMS+stateWindowCeilMS,
		launch.startMS+stateWindowFloorMS, launch.startMS,
		stateScanLimit)
}

// stateUncoveredThreadsQuery asks the one existence question no window question can answer: is
// there a row newer than the matched one that sits outside every live launch's window? Such a
// row is a /new session or a session of a pane that already closed, and its existence alone is
// the refusal. The windows are excluded in the SQL so the answer is a single row or nothing —
// this question never reads a directory's history, so history can never fill it.
func stateUncoveredThreadsQuery(thread stateThread, launches []codexWindow, preLaunchStart int64) string {
	conditions := make([]string, 0, len(launches))
	for _, launch := range launches {
		conditions = append(conditions, fmt.Sprintf(
			"(created_at_ms between %d and %d and (created_at_ms >= %d or updated_at_ms >= %d))",
			launch.startMS-statePreLaunchMS, launch.startMS+stateWindowCeilMS,
			launch.startMS+stateWindowFloorMS, launch.startMS))
	}
	where := fmt.Sprintf("created_at_ms > %d", thread.createdMS)
	if preLaunchStart > 0 {
		where += fmt.Sprintf(" and (created_at_ms >= %d or updated_at_ms >= %d)",
			preLaunchStart, preLaunchStart)
	}
	if len(conditions) > 0 {
		where += " and not (" + strings.Join(conditions, " or ") + ")"
	}
	return "select id || char(9) || rollout_path || char(9) || created_at_ms || char(9) || cwd || " +
		"char(9) || coalesce(updated_at_ms, created_at_ms) from threads " +
		"where originator = 'codex-tui' and cwd = '" + strings.ReplaceAll(thread.cwd, "'", "''") +
		"' and " + where + " order by created_at_ms limit 1"
}

// parseStateThreads reads a query's output: one row per line, tab-joined. A line that does not
// carry all five fields is dropped — a row whose created_at_ms is null prints as an empty
// concatenation, and it is exactly the kind this source must not guess around.
func parseStateThreads(out []byte) []stateThread {
	var threads []stateThread
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 || fields[0] == "" || fields[1] == "" || fields[3] == "" {
			continue
		}
		created, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		updated, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			continue
		}
		threads = append(threads, stateThread{
			id: fields[0], path: fields[1], createdMS: created, updatedMS: updated, cwd: fields[3],
		})
	}
	return threads
}

// unexplainedSuccessor reports whether some row newer than the matched one, in the same
// directory, is not what a live codex launch would have written.
//
// Such a row is a /new session or a session of a pane that is no longer running; in either case
// the matched row may be a conversation this pane has already left, and the moment alone cannot
// say otherwise. The honest answer is a refusal. A launch explains exactly the row it would have
// picked as its own (the window winner, by the same lead rule that matched this pane), so
// windowRows[i] must be the rows of launches[i]'s window and uncovered must already be restricted
// to rows newer than the match that no window holds.
func unexplainedSuccessor(thread stateThread, launches []codexWindow, windowRows [][]stateThread, uncovered []stateThread) bool {
	explained := make(map[string]bool, len(launches))
	for i, launch := range launches {
		if winner, ok := pickStateThread(windowRows[i], []codexWindow{launch}); ok {
			explained[winner.id] = true
		}
	}
	// A row outside every window is explained by nothing: what a launch would have written sits
	// in its window, and there is no launch with a window over this row at all.
	if len(uncovered) > 0 {
		return true
	}
	// A row inside a window that is not that window's winner is a row the launch would not have
	// written first — a /new landing inside its own pane's window, say. It stays unexplained.
	for i := range launches {
		for _, row := range windowRows[i] {
			if row.createdMS > thread.createdMS && !explained[row.id] {
				return true
			}
		}
	}
	return false
}

// pickStateThread decides which candidate can be the pane's own session, and refuses when the
// moment cannot tell them apart.
//
// A launch's own row is born after it, so anything closer to the launch than the floor is an
// earlier launch's row wearing this one's time; anything past the ceiling is some later session.
// Among what remains the earliest wins — it is the row that was born when this launch was — but
// only when it leads the next candidate clearly: two rows within the lead of each other put the
// launches' moments the same distance apart, and naming either would be a coin toss. The lead is
// read off the rows' own creation times, never off how long each took to appear: two rows born
// nearly together are a coin toss however they sat against their launches' clocks.
func pickStateThread(rows []stateThread, windows []codexWindow) (stateThread, bool) {
	best, second := stateThread{}, stateThread{}
	bestFound, secondFound := false, false
	for _, row := range rows {
		if !inStateWindow(row, windows) {
			continue
		}
		if !bestFound || row.createdMS < best.createdMS {
			second, secondFound = best, bestFound
			best, bestFound = row, true
			continue
		}
		if row.id != best.id && (!secondFound || row.createdMS < second.createdMS) {
			second, secondFound = row, true
		}
	}
	if !bestFound {
		return stateThread{}, false
	}
	if secondFound && second.createdMS-best.createdMS < stateWinnerLeadMS {
		return stateThread{}, false
	}
	return best, true
}

// inStateWindow reports whether a row belongs to any of the given launches.
func inStateWindow(row stateThread, windows []codexWindow) bool {
	for _, w := range windows {
		d := row.createdMS - w.startMS
		if d >= stateWindowFloorMS && d <= stateWindowCeilMS {
			return true
		}
		if d >= -statePreLaunchMS && d < stateWindowFloorMS && row.updatedMS >= w.startMS {
			return true
		}
	}
	return false
}
