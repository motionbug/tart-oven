"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");

const html = fs.readFileSync("index.html", "utf8");

function extractFunction(name) {
  let start = html.indexOf("async function " + name + "(");
  if (start === -1) start = html.indexOf("function " + name + "(");
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
    document: { documentElement: { setAttribute() {} }, getElementById() { return null; } },
    localStorage: { setItem() {} },
    latestPerformanceSnapshot: snapshot,
    renderPerformance(value) { redraws++; redrawnSnapshot = value; },
  });
  setTheme(true);
  assert.equal(redraws, 1);
  assert.equal(redrawnSnapshot, snapshot);
});

test("setTheme updates the header toggle face and pressed state", () => {
  const toggle = { textContent: "", title: "", attrs: {}, setAttribute(k, v) { this.attrs[k] = v; } };
  const setTheme = evaluateFunction("setTheme", {
    document: { documentElement: { setAttribute() {} }, getElementById: () => toggle },
    localStorage: { setItem() {} },
    latestPerformanceSnapshot: null,
  });
  setTheme(true);
  assert.equal(toggle.textContent, "\u2600\ufe0f");
  assert.equal(toggle.attrs["aria-pressed"], "true");
  setTheme(false);
  assert.equal(toggle.textContent, "\ud83c\udf19");
  assert.equal(toggle.attrs["aria-pressed"], "false");
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

test("header thresholds stay metric-specific for CPU and RAM", () => {
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
    ["statRam", 0.74, "var(--text)"], ["statRam", 0.75, "var(--amber)"],
    ["statRam", 0.90, "var(--red)"],
  ]) {
    setHeaderStat(id, "value", ratio);
    assert.equal(elements.get(id).style.color, expected, id + " at " + ratio);
  }
});

test("header renders each metric independently when a source is unavailable", () => {
  const elements = new Map();
  const renderHeaderStats = evaluateFunctions(
    ["headerStatColour", "setHeaderStat", "performanceColour", "renderHeaderStats"],
    "renderHeaderStats",
    {
      Math,
      document: {
        getElementById(id) {
          if (!elements.has(id)) elements.set(id, { textContent: "", style: {} });
          return elements.get(id);
        },
      },
    },
  );

  renderHeaderStats({
    cpuAvailable: true, cpuPercent: 34,
    memoryAvailable: false,
    pressureAvailable: true, memoryPressure: "critical",
  });

  assert.equal(elements.get("statCpu").textContent, "34%");
  assert.equal(elements.get("statRam").textContent, "—");
  assert.equal(elements.get("statPressure").textContent, "critical");
  assert.equal(elements.get("statPressure").style.color, "var(--red)");
});

test("mdmCell distinguishes enrolled, unenrolled and never-probed VMs", () => {
  const mdmCell = evaluateFunctions(["consoleURL", "mdmCell"], "mdmCell", {
    esc: (v) => String(v),
  });

  const enrolled = mdmCell({
    mdmCheckedAt: "2026-08-24T12:00:00Z", mdmEnrolled: true,
    mdmServer: "https://emeia.jamfce.com/mdm/ServerURL",
  });
  assert.match(enrolled, /ssh-dot green/);
  assert.match(enrolled, /https:\/\/emeia\.jamfce\.com</,
    "should show the console URL, not the check-in endpoint");
  assert.ok(!/mdm\/ServerURL</.test(enrolled), "raw endpoint must not be the visible text");

  const not = mdmCell({ mdmCheckedAt: "2026-08-24T12:00:00Z", mdmEnrolled: false });
  assert.match(not, /ssh-dot red/);

  // Never probed must be grey, not red: a fresh VM has not been asked yet.
  for (const vm of [{}, { mdmCheckedAt: "0001-01-01T00:00:00Z" }]) {
    const unknown = mdmCell(vm);
    assert.match(unknown, /ssh-dot grey/);
    assert.ok(!/ssh-dot red/.test(unknown), "unknown must not render as not-enrolled");
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

test("SSH guide falls back to saved configuration when its username input is absent", () => {
  const sshGuideUser = evaluateFunction("sshGuideUser", {
    document: { getElementById: () => null },
    latest: { config: { sshUser: "builder" } },
  });
  assert.equal(sshGuideUser(), "builder");
});

test("SSH guide uses its default key path when the key input is absent", () => {
  const sshGuideKey = evaluateFunction("sshGuideKey", {
    document: { getElementById: () => null },
  });
  assert.equal(sshGuideKey(), "~/.ssh/tart-oven");
});

test("SSH guide input binding skips missing optional controls without aborting startup", () => {
  let listeners = 0;
  const sshKey = { addEventListener(name) { if (name === "input") listeners++; } };
  const bindSshGuideInputs = evaluateFunction("bindSshGuideInputs", {
    document: { getElementById: id => id === "sshKey" ? sshKey : null },
    updateSshGuide() {},
  });
  assert.doesNotThrow(() => bindSshGuideInputs());
  assert.equal(listeners, 1);
});

test("Pull OCI modal contains required controls and macOS preset chips", () => {
  const requiredElements = [
    'id="pullOciModal"',
    'id="pullOciBtn"',
    'id="ociImageInput"',
    'id="ociInsecureChk"',
    'id="pullOciSubmit"',
    'id="pullOciCancel"',
    'id="pullOciLog"',
    'data-oci-preset="ghcr.io/cirruslabs/macos-tahoe-base:latest"',
    'data-oci-preset="ghcr.io/cirruslabs/macos-sequoia-base:latest"',
    'data-oci-preset="ghcr.io/cirruslabs/macos-sonoma-base:latest"',
  ];
  for (const el of requiredElements) {
    assert.ok(html.includes(el), `index.html missing element ${el}`);
  }
});

test("openPullOciModal and closePullOciModal toggle modal visibility and focus input", () => {
  const modal = {
    classList: {
      classes: new Set(["hidden"]),
      remove(c) { this.classes.delete(c); },
      add(c) { this.classes.add(c); },
      contains(c) { return this.classes.has(c); },
    },
  };
  const input = { focusCalled: false, focus() { this.focusCalled = true; } };
  const document = {
    getElementById(id) {
      if (id === "pullOciModal") return modal;
      if (id === "ociImageInput") return input;
      return null;
    },
  };

  const openPullOciModal = evaluateFunction("openPullOciModal", {
    document,
    renderTasks() {},
    latest: { tasks: [] },
  });
  const closePullOciModal = evaluateFunction("closePullOciModal", {
    document,
  });

  openPullOciModal();
  assert.equal(modal.classList.contains("hidden"), false);
  assert.equal(input.focusCalled, true);

  closePullOciModal();
  assert.equal(modal.classList.contains("hidden"), true);
});

test("selectOciPreset updates input value with preset dataset", () => {
  const input = { value: "", focusCalled: false, focus() { this.focusCalled = true; } };
  const document = {
    getElementById(id) {
      if (id === "ociImageInput") return input;
      return null;
    },
  };
  const selectOciPreset = evaluateFunction("selectOciPreset", { document });
  const btn = { dataset: { ociPreset: "ghcr.io/cirruslabs/macos-sequoia-base:latest" } };
  selectOciPreset(btn);
  assert.equal(input.value, "ghcr.io/cirruslabs/macos-sequoia-base:latest");
  assert.equal(input.focusCalled, true);
});

test("submitOciPull submits pull request and handles response", async () => {
  let apiPath = "";
  let apiOpts = null;
  const toasts = [];
  const input = { value: "ghcr.io/cirruslabs/macos-sequoia-base:latest" };
  const chk = { checked: true };
  const submitBtn = { disabled: false, textContent: "" };
  const logContainer = {
    classList: {
      classes: new Set(["hidden"]),
      remove(c) { this.classes.delete(c); },
    },
  };
  const document = {
    getElementById(id) {
      if (id === "ociImageInput") return input;
      if (id === "ociInsecureChk") return chk;
      if (id === "pullOciSubmit") return submitBtn;
      if (id === "pullOciLogContainer") return logContainer;
      return null;
    },
  };

  const submitOciPull = evaluateFunction("submitOciPull", {
    document,
    api: async (path, opts) => {
      apiPath = path;
      apiOpts = opts;
      return { ok: true, json: async () => ({ ok: true, taskId: "t-1", image: "ghcr.io/cirruslabs/macos-sequoia-base:latest" }) };
    },
    toast: (title, msg, type) => { toasts.push({ title, msg, type }); },
  });

  await submitOciPull();

  assert.equal(apiPath, "/api/oci/pull");
  assert.equal(apiOpts.method, "POST");
  assert.deepEqual(JSON.parse(apiOpts.body), { image: "ghcr.io/cirruslabs/macos-sequoia-base:latest", insecure: true });
  assert.equal(logContainer.classList.classes.has("hidden"), false);
  assert.equal(submitBtn.textContent, "Pulling...");
});

test("renderTasks streams pull progress to pullOciLog when pull modal is open", () => {
  const elements = {
    tasks: { innerHTML: "" },
    pullOciModal: { classList: { contains(c) { return false; } } },
    pullOciLogContainer: { classList: { remove() {} } },
    pullOciLog: { textContent: "", scrollHeight: 100, scrollTop: 0, clientHeight: 100 },
    pullOciSubmit: { disabled: false, textContent: "" },
    ociImageInput: { value: "ghcr.io/cirruslabs/macos-tahoe-base:latest" },
  };
  const document = {
    getElementById: id => elements[id] || null,
  };
  const renderTasks = evaluateFunction("renderTasks", {
    document,
    esc: s => String(s),
    fmtDateTime: () => "just now",
    cancelTask: () => {},
  });

  const tasks = [
    {
      id: "pull-1",
      kind: "pull",
      target: "ghcr.io/cirruslabs/macos-tahoe-base:latest",
      status: "running",
      output: "Downloading layer 1/5...",
      startedAt: "2026-08-25T12:00:00Z",
    },
  ];

  renderTasks(tasks);
  assert.equal(elements.pullOciLog.textContent, "Downloading layer 1/5...");
  assert.equal(elements.pullOciSubmit.disabled, true);
  assert.equal(elements.pullOciSubmit.textContent, "Pulling...");
});
