#!/usr/bin/env node
// SessionStart hook — inject apm-go harness context into each session.
//
// Adapted from the Trellis SessionStart pattern, scoped to apm-go's harness:
// it reads feature_list.json + .harness/workflow.md and emits a single
// `additionalContext` block containing the current feature state, a workflow
// overview, doc pointers, and a computed Next-Action.
//
// Registered in .claude/settings.json on startup / clear / compact.
// Disable with APM_HARNESS_HOOKS=0 (or CLAUDE_NON_INTERACTIVE=1 for headless).

import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";

function shouldSkip() {
  if (process.env.APM_HARNESS_HOOKS === "0") return true;
  if (process.env.CLAUDE_NON_INTERACTIVE === "1") return true;
  return false;
}

function readStdin() {
  try {
    return fs.readFileSync(0, "utf8");
  } catch {
    return "";
  }
}

function resolveProjectDir(input) {
  const env = process.env.CLAUDE_PROJECT_DIR;
  if (env) return path.resolve(env);
  if (input && typeof input.cwd === "string" && input.cwd) return path.resolve(input.cwd);
  return process.cwd();
}

function readJson(file) {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    return null;
  }
}

function readText(file) {
  try {
    return fs.readFileSync(file, "utf8");
  } catch {
    return "";
  }
}

// Read-only git runtime snapshot (Trellis's <current-state> "dynamic core",
// adapted to apm-go: branch + working-tree dirtiness + recent commits).
// Fail-soft: returns null when not a git repo or git is unavailable.
function gitContext(projectDir) {
  const run = (args) => {
    try {
      return execFileSync("git", args, {
        cwd: projectDir,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
        timeout: 4000,
      }).trim();
    } catch {
      return "";
    }
  };
  const branch = run(["rev-parse", "--abbrev-ref", "HEAD"]);
  if (!branch) return null;
  const status = run(["status", "--porcelain"]);
  const dirty = status ? status.split(/\r?\n/).filter(Boolean) : [];
  const commits = run(["log", "--oneline", "-5"]);
  return { branch, dirty, commits };
}

// Extract a `## <name>` section body up to (but excluding) the next `## ` heading.
function extractSection(content, name) {
  const lines = content.split(/\r?\n/);
  const start = lines.findIndex((l) => l.trim() === `## ${name}`);
  if (start === -1) return "";
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (lines[i].startsWith("## ")) {
      end = i;
      break;
    }
  }
  return lines.slice(start, end).join("\n").trimEnd();
}

function buildWorkflowOverview(projectDir) {
  const wf = readText(path.join(projectDir, ".harness", "workflow.md"));
  if (!wf) return "No .harness/workflow.md found.";
  const toc = wf.split(/\r?\n/).filter((l) => l.startsWith("## "));
  const phaseIndex = extractSection(wf, "Phase Index");
  const out = [
    "# Development Workflow — Section Index",
    "Full guide: .harness/workflow.md (read on demand)",
    "",
    "## Table of Contents",
    ...toc,
    "",
    "---",
    "",
  ];
  if (phaseIndex) out.push(phaseIndex);
  return out.join("\n").trimEnd();
}

function findActiveFeature(featureList) {
  if (!featureList || !Array.isArray(featureList.features)) return null;
  const id = featureList.active_feature;
  return id ? featureList.features.find((f) => f.id === id) || null : null;
}

const FIRST_REPLY_NOTICE = [
  "<first-reply-notice>",
  "On the first visible assistant reply in this session, begin with exactly one short Chinese sentence:",
  "apm-go SessionStart 已注入：workflow（Plan→Execute→Finish）、當前 feature 狀態、git 狀態、Next-Action 已載入。",
  "Then continue directly with the user's request. This notice is one-shot: do not repeat it after the first assistant reply in the same session.",
  "</first-reply-notice>",
].join("\n");

const DONE_GATE =
  "Done-Checklist gate (AGENTS.md): flipping a feature to `done` MUST be verified by a " +
  "FRESH-context subagent (and `copilot --model gpt-5.4` for high-risk claims) — never self-certified.";

function buildTaskStatus(featureList, active) {
  if (!featureList) {
    return [
      "Status: NO feature_list.json",
      "Next-Action: confirm you are at the apm-go repo root; read AGENTS.md Startup Rules.",
    ].join("\n");
  }
  if (!active) {
    const pending = (featureList.features || []).filter((f) => f.status === "pending");
    const next = pending[0];
    return [
      "Status: NO ACTIVE FEATURE",
      next ? `Next pending: ${next.id} — ${next.title}` : "No pending features.",
      "Next-Action: pick the next feature from feature_list.json, then run Phase 1 (Plan) of " +
        ".harness/workflow.md (`/gen-eval-pair <prompt>` drafts the contract).",
      DONE_GATE,
    ].join("\n");
  }

  const lines = [
    `Active feature: ${active.id} — ${active.title}`,
    `Status: ${String(active.status || "unknown").toUpperCase()}`,
  ];
  if (active.done_when) lines.push(`Done when: ${active.done_when}`);
  const remaining = active.remaining || active.known_remaining_minor;
  if (Array.isArray(remaining) && remaining.length) {
    lines.push(`Remaining (${remaining.length}): see feature_list.json "${active.id}".`);
  }

  switch (active.status) {
    case "pending":
      lines.push(
        "Next-Action: Phase 1 (Plan) — read .harness/workspace/PRODUCT.md for scope, run " +
          "`bash init.sh` for a green baseline, then `/gen-eval-pair <prompt>` to draft the contract."
      );
      break;
    case "in_progress":
      lines.push(
        "Next-Action: Phase 2 (Execute) — implement per the /gen-eval-pair contract (you are the writer); " +
          "run `bash init.sh` (each package ≥ 80%)."
      );
      break;
    case "done":
      lines.push(
        "Next-Action: this feature is done. Pick the next pending feature from feature_list.json."
      );
      break;
    default:
      lines.push("Next-Action: read .harness/workflow.md + feature_list.json to determine the phase.");
  }
  lines.push(DONE_GATE);
  return lines.join("\n");
}

function main() {
  if (shouldSkip()) process.exit(0);

  let input = {};
  try {
    const raw = readStdin();
    if (raw) input = JSON.parse(raw);
    if (typeof input !== "object" || input === null) input = {};
  } catch {
    input = {};
  }

  const projectDir = resolveProjectDir(input);
  const featureList = readJson(path.join(projectDir, "feature_list.json"));
  const active = findActiveFeature(featureList);

  const parts = [];
  parts.push(
    "<session-context>",
    "You are starting a session in the apm-go harness (Plan -> Execute -> Finish; the Execute and",
    "Finish gates run through the /gen-eval-pair skill). Read and follow the state and workflow below.",
    "</session-context>",
    ""
  );

  parts.push(FIRST_REPLY_NOTICE, "");

  parts.push("<current-state>");
  if (featureList) {
    if (featureList.project) parts.push(`Project: ${featureList.project}`);
    if (featureList.stage) parts.push(`Stage: ${featureList.stage}`);
    if (featureList.active_feature) parts.push(`Active feature: ${featureList.active_feature}`);
    if (featureList.verification) parts.push(`Verification: ${featureList.verification}`);
  } else {
    parts.push("feature_list.json not found.");
  }
  const git = gitContext(projectDir);
  if (git) {
    parts.push(`Git branch: ${git.branch}`);
    parts.push(`Working tree: ${git.dirty.length ? `${git.dirty.length} file(s) dirty` : "clean"}`);
    if (git.commits) {
      parts.push("Recent commits:");
      for (const line of git.commits.split(/\r?\n/)) parts.push(`  ${line}`);
    }
  }
  parts.push("</current-state>", "");

  parts.push("<workflow>", buildWorkflowOverview(projectDir), "</workflow>", "");

  parts.push(
    "<docs>",
    "Read on demand (do NOT pre-read these in full):",
    "- .harness/workspace/PRODUCT.md — feature scope, stage goals, intentional divergences",
    "- .harness/workspace/ARCHITECTURE.md — Go package layers, data flow, dependency direction, verification strategy",
    "- docs/cli-output-format.md — per-command stdout/stderr format reference",
    "- feature_list.json — all features, status, evidence",
    "- AGENTS.md — Startup Rules, Definition of Done, Done Checklist gate",
    "</docs>",
    ""
  );

  parts.push("<task-status>", buildTaskStatus(featureList, active), "</task-status>", "");

  parts.push(
    "<ready>",
    "Context loaded — workflow overview, current feature state, and doc pointers are injected above; do NOT re-read them.",
    "Follow the Next-Action in <task-status> and the Plan -> Execute -> Finish phases in .harness/workflow.md.",
    "</ready>"
  );

  const result = {
    hookSpecificOutput: {
      hookEventName: "SessionStart",
      additionalContext: parts.join("\n"),
    },
  };
  process.stdout.write(JSON.stringify(result));
}

main();
