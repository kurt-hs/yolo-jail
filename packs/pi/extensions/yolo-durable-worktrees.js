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
// It changes nothing when YOLO_DURABLE_DIR is unset (the host notch, or a launch with no
// durable dir) or when the user already set PI_SUBAGENTS_WORKTREE_DIR or their own
// worktreeBaseDir.
//
// AND IT BOUNDS WHAT IT MOVED (docs/reference/disk-footprint.md "Tool-managed worktrees in the
// durable dir"). pi-subagents removes a run's worktree only when the tree is unchanged or a
// captured patch represents it (cleanupSingleWorktree, src/runs/shared/worktree.ts); work it
// could not capture, a run cut short and a restart all leave theirs, by design: nothing
// pi-subagents cannot prove saved is discarded. In /tmp a restart deleted those; in the durable
// dir nothing did, and one workspace grew 417 of them (31 GB) in a few days. So, at most once
// an hour, the extension starts `yolo-jaild reap-worktrees` detached on the directory it set:
// it removes, with `git worktree remove`, each `pi-worktree-*` tree that is clean, unlocked,
// unused by any process and idle for a day, and lists the dirty ones it kept in
// `<dir>/.yolo-reap.json`. Never on a directory the user chose.
import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

// pi-subagents names each worktree `pi-worktree-<run>-<index>` (src/runs/shared/worktree.ts).
export const WORKTREE_PREFIX = "pi-worktree-";
// The reaper's report, whose mtime is also this rate limit (internal/worktreereap ReportName).
export const REAP_REPORT = ".yolo-reap.json";
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

// startReap starts one detached reaper pass on dir when the last one is older than the
// interval, touching the report first so the many pi processes of one workflow start one pass,
// not one each. It never blocks pi and never fails it: a missing yolo-jaild is ignored.
export function startReap(dir, env = process.env, spawnFn = spawn, now = Date.now()) {
	if (!dir) return false;
	try {
		if (!fs.lstatSync(dir).isDirectory()) return false;
	} catch {
		return false; // no worktree made there yet
	}
	const report = path.join(dir, REAP_REPORT);
	try {
		if (now - fs.statSync(report).mtimeMs < REAP_INTERVAL_MS) return false;
	} catch {}
	try {
		fs.writeFileSync(report, `${JSON.stringify({ started: new Date(now).toISOString() })}\n`, "utf8");
	} catch {
		return false;
	}
	const args = ["reap-worktrees", "--base", dir, "--prefix", WORKTREE_PREFIX];
	const durable = (env.YOLO_DURABLE_DIR || "").replace(/\/+$/, "");
	if (durable) args.push("--repo", path.dirname(path.dirname(durable))); // <workspace>/.yolo/durable
	try {
		const child = spawnFn("yolo-jaild", args, { detached: true, stdio: "ignore", env });
		child.on?.("error", () => {});
		child.unref?.();
	} catch {
		return false;
	}
	return true;
}

export default function registerYoloDurableWorktrees(_pi) {
	const dir = durableWorktreeDir(process.env);
	if (configure(subagentConfigPath(process.env), dir)) {
		startReap(dir, process.env);
	}
}
