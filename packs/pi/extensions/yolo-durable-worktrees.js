// Points pi-subagents' worktrees at the durable dir (docs/reference/jail-home.md#each-agents-own-pack,
// DS-D4). pi-subagents puts a `worktree: true` run's worktrees under os.tmpdir() — /tmp, which
// a jail deletes when it exits — unless `worktreeBaseDir` in
// ~/.pi/agent/extensions/subagent/config.json (or the PI_SUBAGENTS_WORKTREE_DIR environment
// variable) names somewhere else. Configuring subagent/config.json directly rather than setting
// PI_SUBAGENTS_WORKTREE_DIR into process.env ensures child processes (such as test runners
// executing inside the jail) do not inherit the variable and fail assertions expecting default
// worktree behavior.
//
// AN EXTENSION, NOT A PACK `env` VALUE: pack env values are literal, and the durable dir is
// /workspace/.yolo/durable on the container backends and the workspace's real path on
// macos-user. $YOLO_DURABLE_DIR is the launch's own answer, set only when the directory exists.
//
// It changes no setting when YOLO_DURABLE_DIR is unset (the host notch, or a launch with no
// durable dir) or when the user already set PI_SUBAGENTS_WORKTREE_DIR or their own
// worktreeBaseDir.
//
// AND IT BOUNDS WHAT IT MOVED (docs/reference/disk-footprint.md "Tool-managed worktrees in the
// durable dir"). pi-subagents removes a run's worktree only when the tree is unchanged or a
// captured patch represents it (cleanupSingleWorktree, src/runs/shared/worktree.ts); work it
// could not capture, a run cut short and a restart all leave theirs, by design: nothing
// pi-subagents cannot prove saved is discarded. In /tmp a restart deleted those; in the durable
// dir nothing did, and one workspace grew 417 of them (31 GB) in a few days. So, at most once
// an hour, the extension starts `yolo-jaild reap-worktrees` detached on the worktree directory
// in effect when it lies beneath $YOLO_DURABLE_DIR — the one it set, or one the user set there
// (the house rule's PI_SUBAGENTS_WORKTREE_DIR=$YOLO_DURABLE_DIR/worktrees): it removes, with
// `git worktree remove`, each `pi-worktree-*` tree that is clean (no edit the index hides
// either), on a branch, in no git operation, without submodules, unlocked, unused by any jail
// process and idle for a day. The branch stays, so its commits do, unless it is a
// `pi-subagents/*` branch whose tip another branch or a tag holds. What it kept is listed in
// `<dir>/.yolo-reap.json`. Never on a directory outside the durable dir.
import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

// pi-subagents names each worktree `pi-worktree-<run>-<index>` (src/runs/shared/worktree.ts).
export const WORKTREE_PREFIX = "pi-worktree-";
// pi-subagents links the repository's node_modules into each worktree (a symlink no
// `node_modules/` ignore pattern matches), and names each run's branch `pi-subagents/...`.
export const SYNTHETIC = ["node_modules"];
export const BRANCH_PREFIX = "pi-subagents/";
// The reaper's report (internal/worktreereap ReportName) and this rate limit's stamp beside it
// (StampName), kept apart so the report keeps the last pass's findings.
export const REAP_REPORT = ".yolo-reap.json";
export const REAP_STAMP = ".yolo-reap.stamp";
export const REAP_INTERVAL_MS = 60 * 60 * 1000;

export function durableWorktreeDir(env = process.env) {
	const durable = env.YOLO_DURABLE_DIR;
	if (!durable || env.PI_SUBAGENTS_WORKTREE_DIR) return "";
	return durable.replace(/\/+$/, "") + "/worktrees/pi-subagents";
}

export function subagentConfigPath(env = process.env) {
	const configured = env.PI_CODING_AGENT_DIR;
	const home = env.HOME || env.USERPROFILE || os.homedir();
	let agentDir;
	if (configured === "~") {
		agentDir = home;
	} else if (configured?.startsWith("~/") || configured?.startsWith("~\\")) {
		agentDir = path.join(home, configured.slice(2));
	} else if (configured) {
		agentDir = configured;
	} else {
		agentDir = path.join(home, ".pi", "agent");
	}
	return path.join(agentDir, "extensions", "subagent", "config.json");
}

// configure writes or clears the yolo-managed worktreeBaseDir and reports whether the
// directory in effect is the one yolo set (true) or none or the user's own (false).
function configure(configPath, dir) {
	let config = {};
	let fileExists = false;
	try {
		if (fs.existsSync(configPath)) {
			fileExists = true;
			const raw = fs.readFileSync(configPath, "utf8");
			const parsed = JSON.parse(raw);
			if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
				config = parsed;
			}
		}
	} catch {
		return false;
	}

	if (dir) {
		if (config.worktreeBaseDir && !config._yoloManagedWorktreeBaseDir) {
			return false;
		}
		if (config.worktreeBaseDir === dir && config._yoloManagedWorktreeBaseDir === true) {
			return true;
		}
		config.worktreeBaseDir = dir;
		config._yoloManagedWorktreeBaseDir = true;
	} else {
		if (!config._yoloManagedWorktreeBaseDir) {
			return false;
		}
		delete config.worktreeBaseDir;
		delete config._yoloManagedWorktreeBaseDir;
		if (Object.keys(config).length === 0) {
			try {
				if (fileExists) fs.unlinkSync(configPath);
			} catch {}
			return false;
		}
	}

	try {
		fs.mkdirSync(path.dirname(configPath), { recursive: true });
		fs.writeFileSync(configPath, `${JSON.stringify(config, null, "\t")}\n`, "utf8");
	} catch {
		return false;
	}
	return Boolean(dir);
}

function realOrResolved(p) {
	try {
		return fs.realpathSync(p);
	} catch {
		return path.resolve(p);
	}
}

// reapDir is the worktree directory in effect — PI_SUBAGENTS_WORKTREE_DIR, else worktreeBaseDir
// in subagent/config.json (yolo's or the user's) — when it lies strictly beneath
// $YOLO_DURABLE_DIR, and "" otherwise: a directory elsewhere is the user's to bound.
export function reapDir(env = process.env, configPath = subagentConfigPath(env)) {
	const durable = env.YOLO_DURABLE_DIR;
	if (!durable) return "";
	let dir = env.PI_SUBAGENTS_WORKTREE_DIR || "";
	if (!dir) {
		try {
			const parsed = JSON.parse(fs.readFileSync(configPath, "utf8"));
			if (typeof parsed?.worktreeBaseDir === "string") dir = parsed.worktreeBaseDir;
		} catch {}
	}
	if (!dir) return "";
	const rel = path.relative(realOrResolved(durable), realOrResolved(dir));
	if (!rel || rel === ".." || rel.startsWith(`..${path.sep}`) || path.isAbsolute(rel)) return "";
	return dir;
}

// reportFailure replaces the report with why no pass could start and what to do about it, so a
// reader of `.yolo-reap.json` is not left with the last pass's findings and no word of the gap.
function reportFailure(dir, now, err) {
	const failure = {
		at: new Date(now).toISOString(),
		removed: null,
		errors: [`could not start yolo-jaild reap-worktrees: ${err?.message || err}`],
		next:
			"yolo-jaild ships in every jail at /bin/yolo-jaild; restart the jail if it is missing, " +
			`or run \`yolo-jaild reap-worktrees --base ${dir} --prefix ${WORKTREE_PREFIX}\` yourself`,
	};
	try {
		fs.writeFileSync(path.join(dir, REAP_REPORT), `${JSON.stringify(failure, null, 2)}\n`, "utf8");
	} catch {}
}

// startReap starts one detached reaper pass on dir when the stamp is older than the interval,
// touching the stamp first so the many pi processes of one workflow start one pass, not one
// each. It never blocks pi and never fails it: a yolo-jaild that cannot start is recorded in the
// report with the next step.
export function startReap(dir, env = process.env, spawnFn = spawn, now = Date.now()) {
	if (!dir) return false;
	try {
		if (!fs.lstatSync(dir).isDirectory()) return false;
	} catch {
		return false; // no worktree made there yet
	}
	const stamp = path.join(dir, REAP_STAMP);
	try {
		if (now - fs.statSync(stamp).mtimeMs < REAP_INTERVAL_MS) return false;
	} catch {}
	try {
		fs.writeFileSync(stamp, `${new Date(now).toISOString()}\n`, "utf8");
	} catch {
		return false;
	}
	const args = ["reap-worktrees", "--base", dir, "--prefix", WORKTREE_PREFIX];
	for (const name of SYNTHETIC) args.push("--synthetic", name);
	args.push("--branch-prefix", BRANCH_PREFIX);
	const durable = (env.YOLO_DURABLE_DIR || "").replace(/\/+$/, "");
	if (durable) args.push("--repo", path.dirname(path.dirname(durable))); // <workspace>/.yolo/durable
	try {
		const child = spawnFn("yolo-jaild", args, { detached: true, stdio: "ignore", env });
		child.on?.("error", (err) => reportFailure(dir, now, err));
		child.unref?.();
	} catch (err) {
		reportFailure(dir, now, err);
		return false;
	}
	return true;
}

export default function registerYoloDurableWorktrees(_pi) {
	const configPath = subagentConfigPath(process.env);
	configure(configPath, durableWorktreeDir(process.env));
	startReap(reapDir(process.env, configPath), process.env);
}
