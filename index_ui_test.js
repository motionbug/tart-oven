"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");

const html = fs.readFileSync("index.html", "utf8");

function extractFunction(name) {
  const start = html.indexOf("function " + name + "(");
  assert.notEqual(start, -1, "missing function " + name);
  const open = html.indexOf("{", start);
  let depth = 0;
  let quote = "";
  let escaped = false;
  for (let index = open; index < html.length; index++) {
    const char = html[index];
    if (quote) {
      if (escaped) escaped = false;
      else if (char === "\\") escaped = true;
      else if (char === quote) quote = "";
      continue;
    }
    if (char === "\"" || char === "'" || char === "`") { quote = char; continue; }
    if (char === "{") depth++;
    if (char === "}" && --depth === 0) return html.slice(start, index + 1);
  }
  throw new Error("unterminated function " + name);
}

function evaluateFunction(name, globals) {
  const context = vm.createContext(Object.assign({}, globals));
  vm.runInContext(extractFunction(name) + "; globalThis.testFunction = " + name + ";", context);
  return context.testFunction;
}

function evaluateFunctions(names, exportedName, globals) {
  const context = vm.createContext(Object.assign({}, globals));
  const definitions = names.map(extractFunction).join("\n");
  vm.runInContext(definitions + "; globalThis.testFunction = " + exportedName + ";", context);
  return context.testFunction;
}

function chartFixture() {
  const calls = [];
  const context = {};
  ["setTransform", "clearRect", "beginPath", "moveTo", "lineTo", "stroke", "fillText", "arc", "fill", "closePath", "fillRect"].forEach(name => {
    context[name] = (...args) => calls.push([name, ...args]);
  });
  const canvas = {
    clientWidth: 300,
    width: 0,
    height: 0,
    getContext: () => context,
  };
  const styles = {
    "--accent": "#4f8cff", "--amber": "#ffb74d", "--green": "#2ecc71",
    "--red": "#ff5c5c", "--border": "#2a2f3a", "--muted": "#9aa3b2",
  };
  const drawLineChart = evaluateFunction("drawLineChart", {
    window: { devicePixelRatio: 1 },
    document: { documentElement: {} },
    getComputedStyle: () => ({ getPropertyValue: property => styles[property] || "" }),
  });
  return { calls, canvas, drawLineChart };
}

test("drawLineChart renders an isolated finite sample as a point", () => {
  const fixture = chartFixture();
  fixture.drawLineChart(fixture.canvas, [{ values: [42], colour: "--accent" }], {
    min: 0, max: 100, timestamps: ["2026-08-22T12:00:00Z"],
  });
  assert.equal(fixture.calls.filter(call => call[0] === "arc").length, 1);
  assert.equal(fixture.calls.filter(call => call[0] === "fill").length, 1);
});

test("drawLineChart gives every pressure state a distinct marker shape", () => {
  const fixture = chartFixture();
  fixture.drawLineChart(fixture.canvas, [{ values: [40, 41, 42], colour: "--green" }], {
    min: 0,
    max: 100,
    timestamps: ["2026-08-22T12:00:00Z", "2026-08-22T12:01:00Z", "2026-08-22T12:02:00Z"],
    markers: [
      { state: "normal", colour: "--green" },
      { state: "warning", colour: "--amber" },
      { state: "critical", colour: "--red" },
    ],
  });
  assert.equal(fixture.calls.filter(call => call[0] === "arc").length, 1, "normal should be a circle");
  assert.equal(fixture.calls.filter(call => call[0] === "closePath").length, 1, "warning should be a triangle");
  assert.equal(fixture.calls.filter(call => call[0] === "fillRect").length, 1, "critical should be a square");
});

test("setTheme immediately redraws a cached performance snapshot", () => {
  const snapshot = { latest: { cpuPercent: 25 }, history: [] };
  let redraws = 0;
  let redrawnSnapshot = null;
  const setTheme = evaluateFunction("setTheme", {
    document: { documentElement: { setAttribute() {} } },
    localStorage: { setItem() {} },
    latestPerformanceSnapshot: snapshot,
    renderPerformance(value) { redraws++; redrawnSnapshot = value; },
  });
  setTheme(true);
  assert.equal(redraws, 1);
  assert.equal(redrawnSnapshot, snapshot);
});

test("performanceColour keeps thresholds associated with their metric", () => {
  const performanceColour = evaluateFunction("performanceColour", {});
  for (const [kind, value, expected] of [
    ["cpu", 80, "var(--green)"], ["cpu", 81, "var(--amber)"],
    ["cpu", 95, "var(--amber)"], ["cpu", 96, "var(--red)"],
    ["disk", 80, "var(--green)"], ["disk", 81, "var(--amber)"],
    ["disk", 90, "var(--amber)"], ["disk", 91, "var(--red)"],
    ["pressure", "normal", "var(--green)"],
    ["pressure", "warning", "var(--amber)"],
    ["pressure", "critical", "var(--red)"],
  ]) {
    assert.equal(performanceColour(kind, value), expected, kind + " at " + value);
  }
});

test("compact header keeps CPU and disk thresholds metric-specific while preserving RAM", () => {
  const elements = new Map();
  const setHeaderStat = evaluateFunctions(["headerStatColour", "setHeaderStat"], "setHeaderStat", {
    document: {
      getElementById(id) {
        if (!elements.has(id)) elements.set(id, { textContent: "", style: {} });
        return elements.get(id);
      },
    },
  });

  for (const [id, ratio, expected] of [
    ["statCpu", 0.80, "var(--text)"], ["statCpu", 0.81, "var(--amber)"],
    ["statCpu", 0.95, "var(--amber)"], ["statCpu", 0.96, "var(--red)"],
    ["statDisk", 0.80, "var(--text)"], ["statDisk", 0.81, "var(--amber)"],
    ["statDisk", 0.90, "var(--amber)"], ["statDisk", 0.91, "var(--red)"],
    ["statRam", 0.74, "var(--text)"], ["statRam", 0.75, "var(--amber)"],
    ["statRam", 0.90, "var(--red)"],
  ]) {
    setHeaderStat(id, "value", ratio);
    assert.equal(elements.get(id).style.color, expected, id + " at " + ratio);
  }
});

test("renderOCIImages hides cached images for running-only and renders clone-only rows", () => {
  const panel = { classList: { toggle(name, enabled) { panel.hidden = name === "hidden" && enabled; } } };
  const buttons = [];
  const rows = { innerHTML: "", querySelectorAll: () => buttons };
  const renderOCIImages = evaluateFunction("renderOCIImages", {
    document: { getElementById: id => id === "ociPanel" ? panel : rows },
    esc: value => String(value),
    fmtAgo: () => "1h ago",
  });
  const images = [{
    source: "OCI", name: "ghcr.io/example/base@sha256:abc", size: 22, disk: 80,
    accessed: "2026-08-23T12:00:00Z",
  }];

  renderOCIImages(images, false, "sha256");
  assert.match(rows.innerHTML, /ghcr\.io\/example\/base@sha256:abc/);
  assert.match(rows.innerHTML, />Clone<\/button>/);
  assert.doesNotMatch(rows.innerHTML, /<button[^>]+onclick=/);
  for (const forbidden of [">Run</button>", ">Stop</button>", ">Edit</button>", ">Screen</button>"]) {
    assert.doesNotMatch(rows.innerHTML, new RegExp(forbidden));
  }

  renderOCIImages(images, true, "");
  assert.equal(panel.hidden, true);
});

test("renderOCIImages safely binds a hostile external name without inline JavaScript", () => {
  const hostile = `ghcr.io/example/\" onmouseover=\"alert(1)`;
  const button = { dataset: { ociName: hostile }, onclick: null };
  const rows = { innerHTML: "", querySelectorAll: () => [button] };
  let selected = "";
  const renderOCIImages = evaluateFunction("renderOCIImages", {
    document: { getElementById: id => id === "ociPanel" ? { classList: { toggle() {} } } : rows },
    esc: value => String(value).replaceAll("&", "&amp;").replaceAll('"', "&quot;"),
    fmtAgo: () => "1h ago",
    cloneFromOCI: name => { selected = name; },
  });

  renderOCIImages([{ name: hostile, source: "OCI", size: 1, disk: 2, accessed: "2026-08-23T12:00:00Z" }], false, "");
  assert.match(rows.innerHTML, /data-oci-name="ghcr\.io\/example\/&quot; onmouseover=&quot;alert\(1\)"/);
  assert.doesNotMatch(rows.innerHTML, /<button[^>]+onclick=/);
  button.onclick();
  assert.equal(selected, hostile);
});

test("cloneFromOCI opens clone management with the exact OCI reference", () => {
  const source = { value: "", focusCalled: false, focus() { this.focusCalled = true; } };
  const cloneRadio = { checked: false };
  let shown = "";
  let modeUpdates = 0;
  const cloneFromOCI = evaluateFunction("cloneFromOCI", {
    showTab: id => { shown = id; },
    updateCreateMode: () => { modeUpdates++; },
    document: {
      querySelector: () => cloneRadio,
      getElementById: () => source,
    },
  });
  const exact = "ghcr.io/example/base@sha256:abc";

  cloneFromOCI(exact);

  assert.equal(shown, "vmm");
  assert.equal(cloneRadio.checked, true);
  assert.equal(modeUpdates, 1);
  assert.equal(source.value, exact);
  assert.equal(source.focusCalled, true);
});

test("SSH and MDM helpers ignore running OCI entries", () => {
  const elements = {
    sshTarget: { value: "", innerHTML: "" },
    sshGuideVm: { value: "", innerHTML: "" },
    copyMdmBtn: { disabled: false },
  };
  const document = { getElementById: id => elements[id] };
  const vms = [
    { name: "ghcr.io/example/base:latest", source: "OCI", state: "running", ip: "192.0.2.10" },
    { name: "local-stopped", source: "local", state: "stopped", ip: "" },
  ];
  const globals = {
    document,
    esc: value => String(value),
    isOCI: source => String(source || "").trim().toLowerCase() === "oci",
    updateSshGuide() {},
    mdmCopyInFlight: false,
    latest: { vms },
  };

  evaluateFunction("fillSshTargets", globals)(vms);
  assert.doesNotMatch(elements.sshTarget.innerHTML, /ghcr\.io/);
  evaluateFunction("fillSshGuide", globals)(vms);
  assert.doesNotMatch(elements.sshGuideVm.innerHTML, /ghcr\.io/);
  evaluateFunction("updateMdmCopyButton", globals)(vms);
  assert.equal(elements.copyMdmBtn.disabled, true);
});
