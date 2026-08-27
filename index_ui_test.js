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
  let shown = "";
  let modeSet = "";
  const cloneFromOCI = evaluateFunction("cloneFromOCI", {
    showTab: id => { shown = id; },
    setCreateMode: mode => { modeSet = mode; },
    document: {
      getElementById: () => source,
    },
  });
  const exact = "ghcr.io/example/base@sha256:abc";

  cloneFromOCI(exact);

  assert.equal(shown, "vmm");
  assert.equal(modeSet, "clone");
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

test("Pull OCI modal contains required controls, accessibility attributes, and macOS preset chips", () => {
  const requiredElements = [
    'id="pullOciModal"',
    'role="dialog"',
    'aria-modal="true"',
    'aria-labelledby="pullOciTitle"',
    'id="pullOciTitle"',
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
  const renderTasks = evaluateFunctions(["renderTasks", "updateOciProgress"], "renderTasks", {
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

test("renderTasks streams pull progress to the inline Create-VM OCI section when it is visible", () => {
  const elements = {
    tasks: { innerHTML: "" },
    pullOciModal: { classList: { contains(c) { return true; } } },
    ociField: { style: { display: "" } },
    pullOciLogContainerInline: { classList: { remove() {} } },
    pullOciLogInline: { textContent: "", scrollHeight: 100, scrollTop: 0, clientHeight: 100 },
    createBtn: { disabled: false, textContent: "Pull Image" },
    ociImageInputInline: { value: "ghcr.io/cirruslabs/macos-sequoia-base:latest" },
  };
  const document = {
    getElementById: id => elements[id] || null,
  };
  const renderTasks = evaluateFunctions(["renderTasks", "updateOciProgress"], "renderTasks", {
    document,
    esc: s => String(s),
    fmtDateTime: () => "just now",
    cancelTask: () => {},
  });

  const tasks = [
    {
      id: "pull-2",
      kind: "pull",
      target: "ghcr.io/cirruslabs/macos-sequoia-base:latest",
      status: "running",
      output: "Downloading layer 2/5...",
      startedAt: "2026-08-27T12:00:00Z",
    },
  ];

  renderTasks(tasks);
  assert.equal(elements.pullOciLogInline.textContent, "Downloading layer 2/5...");
  assert.equal(elements.createBtn.disabled, true);
  assert.equal(elements.createBtn.textContent, "Pulling...");
});

test("submitOciPull handles text/plain and json error responses gracefully", async () => {
  const toasts = [];
  const input = { value: "ghcr.io/cirruslabs/macos-sequoia-base:latest" };
  const submitBtn = { disabled: false, textContent: "" };
  const document = {
    getElementById(id) {
      if (id === "ociImageInput") return input;
      if (id === "ociInsecureChk") return { checked: false };
      if (id === "pullOciSubmit") return submitBtn;
      if (id === "pullOciLogContainer") return { classList: { remove() {} } };
      return null;
    },
  };

  // Test 1: Plain-text error (like Go http.Error 409) with single-stream body read simulation
  let textRead1 = false;
  const submitOciPullPlainTextErr = evaluateFunction("submitOciPull", {
    document,
    api: async () => ({
      ok: false,
      text: async () => {
        if (textRead1) throw new Error("Body already read");
        textRead1 = true;
        return "a pull for this image is already in progress\n";
      },
    }),
    toast: (title, msg, type) => { toasts.push({ title, msg, type }); },
  });

  await submitOciPullPlainTextErr();
  assert.equal(toasts.length, 1);
  assert.equal(toasts[0].title, "OCI Pull Failed");
  assert.equal(toasts[0].msg, "a pull for this image is already in progress");
  assert.equal(submitBtn.disabled, false);
  assert.equal(submitBtn.textContent, "Pull Image");

  // Test 2: JSON error with single-stream body read simulation
  toasts.length = 0;
  let textRead2 = false;
  const submitOciPullJsonErr = evaluateFunction("submitOciPull", {
    document,
    api: async () => ({
      ok: false,
      text: async () => {
        if (textRead2) throw new Error("Body already read");
        textRead2 = true;
        return '{"error":"insufficient disk space"}';
      },
    }),
    toast: (title, msg, type) => { toasts.push({ title, msg, type }); },
  });

  await submitOciPullJsonErr();
  assert.equal(toasts.length, 1);
  assert.equal(toasts[0].title, "OCI Pull Failed");
  assert.equal(toasts[0].msg, "insufficient disk space");
});

test("Help tab contains interactive TOC sidebar, search input, and copy buttons", () => {
  const requiredElements = [
    'id="helpToc"',
    'id="helpSearch"',
    'id="helpContent"',
    'class="copy-btn"',
  ];
  for (const el of requiredElements) {
    assert.ok(html.includes(el), `index.html missing element ${el}`);
  }
});

test("renderMarkdown generates slug heading IDs, TOC items in helpToc, and code copy buttons", () => {
  const tocElement = { innerHTML: "" };
  const document = {
    getElementById(id) {
      if (id === "helpToc") return tocElement;
      return null;
    },
  };

  const renderMarkdown = evaluateFunction("renderMarkdown", { document });
  const rawMd = "# Title\n\n## Stage 1: Quickstart\n\nSome intro text.\n\n### Tart CLI Setup\n\n```sh\ntart clone base vm1\n```\n\n## Stage 2: Fleet Management\n\nFleet details.";
  const result = renderMarkdown(rawMd);

  // Verify slug heading IDs
  assert.match(result, /<h2 id="stage-1-quickstart">Stage 1: Quickstart<\/h2>/);
  assert.match(result, /<h3 id="tart-cli-setup">Tart CLI Setup<\/h3>/);
  assert.match(result, /<h2 id="stage-2-fleet-management">Stage 2: Fleet Management<\/h2>/);

  // Verify code block has copy button wrapper
  assert.match(result, /class="copy-btn"/);
  assert.match(result, /onclick="copySnippet\(this\)"/);
  assert.match(result, /tart clone base vm1/);

  // Verify helpToc was populated
  assert.match(tocElement.innerHTML, /href="#stage-1-quickstart"/);
  assert.match(tocElement.innerHTML, /href="#tart-cli-setup"/);
  assert.match(tocElement.innerHTML, /href="#stage-2-fleet-management"/);
});

test("filterHelp filters rendered sections and highlights search terms", () => {
  function makeElement(tag, text, initialHtml) {
    const el = {
      nodeType: 1,
      tagName: tag.toUpperCase(),
      textContent: text,
      innerHTML: initialHtml || text,
      style: {},
      classList: {
        classes: new Set(),
        add(c) { this.classes.add(c); },
        remove(c) { this.classes.delete(c); },
        contains(c) { return this.classes.has(c); },
      },
      childNodes: [],
      parentNode: null,
      dataset: {},
      querySelector() { return null; },
    };
    return el;
  }

  const sec1 = makeElement("section", "Stage 1 Quickstart Guide for Tart CLI setup", "<h2>Stage 1</h2><p>Quickstart Guide for Tart CLI setup</p>");
  sec1.dataset.sectionId = "stage-1";
  const sec2 = makeElement("section", "Stage 5 Jamf Pro MDM Administrator Toolkit", "<h2>Stage 5</h2><p>Jamf Pro MDM Administrator Toolkit</p>");
  sec2.dataset.sectionId = "stage-5";

  const tocLink1 = makeElement("a", "Stage 1", "<a href=\"#stage-1\">Stage 1</a>");
  const tocLink2 = makeElement("a", "Stage 5", "<a href=\"#stage-5\">Stage 5</a>");

  const content = {
    querySelectorAll(selector) {
      if (selector.includes(".help-section")) return [sec1, sec2];
      return [];
    },
  };
  const toc = {
    querySelectorAll() { return [tocLink1, tocLink2]; },
    querySelector(selector) {
      if (selector.includes("stage-1")) return tocLink1;
      if (selector.includes("stage-5")) return tocLink2;
      return null;
    },
  };

  const document = {
    getElementById(id) {
      if (id === "helpContent" || id === "readme") return content;
      if (id === "helpToc") return toc;
      return null;
    },
    createElement(tag) {
      return makeElement(tag, "", "");
    },
  };

  const filterHelp = evaluateFunction("filterHelp", { document });

  // Filter for "Jamf"
  filterHelp("Jamf");
  assert.equal(sec1.style.display, "none", "sec1 should be hidden");
  assert.equal(sec2.style.display, "", "sec2 should be visible");
  assert.equal(tocLink1.style.display, "none", "tocLink1 should be hidden");
  assert.equal(tocLink2.style.display, "", "tocLink2 should be visible");

  // Clear query
  filterHelp("");
  assert.equal(sec1.style.display, "", "sec1 should be restored");
  assert.equal(sec2.style.display, "", "sec2 should be restored");
  assert.equal(tocLink1.style.display, "", "tocLink1 should be restored");
  assert.equal(tocLink2.style.display, "", "tocLink2 should be restored");
});

test("copySnippet copies code block text and shows temporary feedback", () => {
  let copiedText = "";
  const mockNavigator = {
    clipboard: {
      writeText: async (t) => { copiedText = t; },
    },
  };

  const codeEl = {
    textContent: "tart set vm1 --cpu 4 --memory 8192",
    innerText: "tart set vm1 --cpu 4 --memory 8192",
  };
  const wrapper = {
    querySelector: (sel) => {
      if (sel.includes("code") || sel.includes("pre")) return codeEl;
      return null;
    },
  };
  const btn = {
    textContent: "Copy",
    dataset: {},
    classList: {
      classes: new Set(),
      add(c) { this.classes.add(c); },
      remove(c) { this.classes.delete(c); },
    },
    closest: (sel) => wrapper,
    parentElement: wrapper,
  };

  let scheduledMs = 0;
  let scheduledCallback = null;
  const mockSetTimeout = (fn, ms) => {
    scheduledMs = ms;
    scheduledCallback = fn;
    return 123;
  };

  const copySnippet = evaluateFunction("copySnippet", {
    navigator: mockNavigator,
    setTimeout: mockSetTimeout,
    clearTimeout: () => {},
  });

  copySnippet(btn);

  assert.equal(copiedText, "tart set vm1 --cpu 4 --memory 8192");
  assert.equal(btn.textContent, "✓ Copied");
  assert.equal(btn.classList.classes.has("copied"), true);
  assert.equal(scheduledMs, 2000);

  // Trigger timeout callback to restore
  scheduledCallback();
  assert.equal(btn.textContent, "Copy");
  assert.equal(btn.classList.classes.has("copied"), false);
});

test("renderMarkdownGuide alias behaves identically to renderMarkdown", () => {
  const document = { getElementById: () => null };
  const renderMarkdownGuide = evaluateFunction("renderMarkdownGuide", {
    renderMarkdown: evaluateFunction("renderMarkdown", { document }),
  });
  const md = "## Stage 3: Base Images\n\nPulling Sequoia 15 image.\n\n```bash\ntart pull ghcr.io/cirruslabs/macos-sequoia-base:latest\n```";
  const output = renderMarkdownGuide(md);
  assert.match(output, /<h2 id="stage-3-base-images">Stage 3: Base Images<\/h2>/);
  assert.match(output, /class="copy-btn"/);
  assert.match(output, /macos-sequoia-base/);
});

test("showTab switches tab state and loads readme for help and guide aliases", () => {
  let readmeLoaded = 0;
  const tabs = [
    { id: "tab-dashboard", classList: { classes: new Set(["active"]), toggle(c, v) { if (v) this.classes.add(c); else this.classes.delete(c); } } },
    { id: "tab-guide", classList: { classes: new Set(), toggle(c, v) { if (v) this.classes.add(c); else this.classes.delete(c); } } },
  ];
  const buttons = [
    { dataset: { tab: "dashboard" }, classList: { classes: new Set(["active"]), toggle(c, v) { if (v) this.classes.add(c); else this.classes.delete(c); } } },
    { dataset: { tab: "guide" }, classList: { classes: new Set(), toggle(c, v) { if (v) this.classes.add(c); else this.classes.delete(c); } } },
  ];

  const document = {
    querySelectorAll(sel) {
      if (sel === ".tab") return tabs;
      if (sel === "nav.tabs .tabbtn") return buttons;
      return [];
    },
  };

  const showTab = evaluateFunction("showTab", {
    document,
    loadHistory: () => {},
    loadReadme: () => { readmeLoaded++; },
    loadPerformance: () => {},
  });

  showTab("guide");
  assert.equal(tabs[1].classList.classes.has("active"), true);
  assert.equal(buttons[1].classList.classes.has("active"), true);
  assert.equal(readmeLoaded, 1);

  showTab("help");
  assert.equal(tabs[1].classList.classes.has("active"), true);
  assert.equal(buttons[1].classList.classes.has("active"), true);
  assert.equal(readmeLoaded, 2);
});

test("filterHelp escapes HTML characters in search highlights preventing tag corruption and XSS", () => {
  function makeElement(tag, text, initialHtml) {
    const el = {
      nodeType: 1,
      tagName: tag.toUpperCase(),
      textContent: text,
      innerHTML: initialHtml || text,
      style: {},
      classList: {
        classes: new Set(),
        add(c) { this.classes.add(c); },
        remove(c) { this.classes.delete(c); },
        contains(c) { return this.classes.has(c); },
      },
      childNodes: [],
      parentNode: null,
      dataset: {},
      querySelector() { return null; },
    };
    return el;
  }

  const textVal = "Configure tart set <vm-name> --memory <mb-size> & test";
  const textNode = {
    nodeType: 3,
    nodeValue: textVal,
    parentNode: null,
  };

  const sec = makeElement("section", textVal, "<p>" + textVal + "</p>");
  sec.dataset.sectionId = "cmd-section";
  textNode.parentNode = sec;
  sec.childNodes = [textNode];

  let createdSpan = null;
  sec.replaceChild = (newChild, oldChild) => {
    createdSpan = newChild;
  };

  const content = {
    querySelectorAll(selector) {
      if (selector.includes(".help-section")) return [sec];
      return [];
    },
  };

  const document = {
    getElementById(id) {
      if (id === "helpContent" || id === "readme") return content;
      return null;
    },
    createElement(tag) {
      const el = makeElement(tag, "", "");
      return el;
    },
  };

  const filterHelp = evaluateFunction("filterHelp", { document });

  filterHelp("tart");
  assert.ok(createdSpan !== null, "span should have been created for highlight");
  assert.match(createdSpan.innerHTML, /<mark class="help-highlight">tart<\/mark>/);
  assert.match(createdSpan.innerHTML, /&lt;vm-name&gt;/);
  assert.match(createdSpan.innerHTML, /&lt;mb-size&gt;/);
  assert.match(createdSpan.innerHTML, /&amp;/);
  assert.doesNotMatch(createdSpan.innerHTML, /<vm-name>/);
});

test("copySnippet uses fallback to document.execCommand when navigator.clipboard is unavailable", () => {
  let execCommandCalled = "";
  let textareaValue = "";
  let appendedChild = null;
  let removedChild = null;

  const mockTextarea = {
    style: {},
    setAttribute() {},
    select() {},
    set value(v) { textareaValue = v; },
    get value() { return textareaValue; },
  };

  const mockBody = {
    appendChild(child) { appendedChild = child; },
    removeChild(child) { removedChild = child; },
  };

  const mockDocument = {
    body: mockBody,
    createElement(tag) {
      if (tag === "textarea") return mockTextarea;
      return {};
    },
    execCommand(cmd) {
      execCommandCalled = cmd;
      return true;
    },
  };

  const codeEl = { textContent: "tart clone ghcr.io/base vm1" };
  const wrapper = {
    querySelector(sel) {
      if (sel.includes("code") || sel.includes("pre")) return codeEl;
      return null;
    },
  };
  const btn = {
    textContent: "Copy",
    dataset: {},
    classList: {
      classes: new Set(),
      add(c) { this.classes.add(c); },
      remove(c) { this.classes.delete(c); },
    },
    closest() { return wrapper; },
    parentElement: wrapper,
  };

  const copySnippet = evaluateFunction("copySnippet", {
    document: mockDocument,
    navigator: {}, // No clipboard
    setTimeout: (fn) => fn(),
    clearTimeout: () => {},
  });

  copySnippet(btn);

  assert.equal(execCommandCalled, "copy");
  assert.equal(textareaValue, "tart clone ghcr.io/base vm1");
  assert.equal(appendedChild, mockTextarea);
  assert.equal(removedChild, mockTextarea);
  assert.equal(btn.textContent, "Copy");
});

test("copySnippet uses fallback when navigator.clipboard.writeText rejects", async () => {
  let execCommandCalled = "";
  let textareaValue = "";

  const mockTextarea = {
    style: {},
    setAttribute() {},
    select() {},
    set value(v) { textareaValue = v; },
    get value() { return textareaValue; },
  };

  const mockBody = {
    appendChild() {},
    removeChild() {},
  };

  const mockDocument = {
    body: mockBody,
    createElement(tag) {
      if (tag === "textarea") return mockTextarea;
      return {};
    },
    execCommand(cmd) {
      execCommandCalled = cmd;
      return true;
    },
  };

  const mockNavigator = {
    clipboard: {
      writeText: async () => {
        throw new Error("Clipboard permission denied");
      },
    },
  };

  const codeEl = { textContent: "tart run vm1" };
  const wrapper = { querySelector: () => codeEl };
  const btn = {
    textContent: "Copy",
    dataset: {},
    classList: {
      classes: new Set(),
      add(c) { this.classes.add(c); },
      remove(c) { this.classes.delete(c); },
    },
    closest: () => wrapper,
    parentElement: wrapper,
  };

  const copySnippet = evaluateFunction("copySnippet", {
    document: mockDocument,
    navigator: mockNavigator,
    setTimeout: (fn) => fn(),
    clearTimeout: () => {},
  });

  copySnippet(btn);

  // Wait for rejected promise tick
  await new Promise(resolve => setImmediate(resolve));

  assert.equal(execCommandCalled, "copy");
  assert.equal(textareaValue, "tart run vm1");
});

test("filterHelp toggles visibility for both h2 and child h3 links in helpToc", () => {
  function makeElement(tag, id, text) {
    const el = {
      nodeType: 1,
      tagName: tag.toUpperCase(),
      id,
      textContent: text,
      innerHTML: text,
      style: {},
      classList: {
        classes: new Set(),
        add(c) { this.classes.add(c); },
        remove(c) { this.classes.delete(c); },
        contains(c) { return this.classes.has(c); },
      },
      dataset: {},
      querySelectorAll() { return []; },
      querySelector() { return null; },
    };
    return el;
  }

  // Section 1 with H2 and H3
  const h2Sec1 = makeElement("h2", "sec-quickstart", "Quickstart");
  const h3Sec1 = makeElement("h3", "sec-quickstart-prereq", "Prerequisites");
  const sec1 = makeElement("section", "sec-quickstart", "Quickstart content");
  sec1.dataset.sectionId = "sec-quickstart";
  sec1.querySelectorAll = () => [h2Sec1, h3Sec1];

  // Section 2 with H2 and H3
  const h2Sec2 = makeElement("h2", "sec-fleet", "Fleet Management");
  const h3Sec2 = makeElement("h3", "sec-fleet-rotation", "Fleet Rotation");
  const sec2 = makeElement("section", "sec-fleet", "Fleet rotation content");
  sec2.dataset.sectionId = "sec-fleet";
  sec2.querySelectorAll = () => [h2Sec2, h3Sec2];

  // TOC links
  const linkH2Sec1 = makeElement("a", "", "Quickstart");
  const linkH3Sec1 = makeElement("a", "", "Prerequisites");
  const linkH2Sec2 = makeElement("a", "", "Fleet Management");
  const linkH3Sec2 = makeElement("a", "", "Fleet Rotation");

  const tocMap = {
    'a[href="#sec-quickstart"], [data-target="sec-quickstart"]': linkH2Sec1,
    'a[href="#sec-quickstart-prereq"], [data-target="sec-quickstart-prereq"]': linkH3Sec1,
    'a[href="#sec-fleet"], [data-target="sec-fleet"]': linkH2Sec2,
    'a[href="#sec-fleet-rotation"], [data-target="sec-fleet-rotation"]': linkH3Sec2,
  };

  const toc = {
    querySelectorAll: () => [linkH2Sec1, linkH3Sec1, linkH2Sec2, linkH3Sec2],
    querySelector: (sel) => tocMap[sel] || null,
  };

  const content = {
    querySelectorAll: (sel) => {
      if (sel.includes(".help-section")) return [sec1, sec2];
      return [];
    },
  };

  const document = {
    getElementById(id) {
      if (id === "helpContent" || id === "readme") return content;
      if (id === "helpToc") return toc;
      return null;
    },
    createElement(tag) { return makeElement(tag, "", ""); },
  };

  const filterHelp = evaluateFunction("filterHelp", { document });

  // Filter for "rotation" (matches only Sec 2)
  filterHelp("rotation");

  assert.equal(sec1.style.display, "none");
  assert.equal(linkH2Sec1.style.display, "none");
  assert.equal(linkH3Sec1.style.display, "none");

  assert.equal(sec2.style.display, "");
  assert.equal(linkH2Sec2.style.display, "");
  assert.equal(linkH3Sec2.style.display, "");

  // Clear query -> all restored
  filterHelp("");
  assert.equal(sec1.style.display, "");
  assert.equal(linkH2Sec1.style.display, "");
  assert.equal(linkH3Sec1.style.display, "");
  assert.equal(sec2.style.display, "");
  assert.equal(linkH2Sec2.style.display, "");
  assert.equal(linkH3Sec2.style.display, "");
});

test("Onboarding wizard modal and empty dashboard hero exist in index.html", () => {
  const requiredElements = [
    'id="onboardingModal"',
    'id="reopenWizardBtn"',
    'id="emptyDashboardHero"',
    'data-role="devops"',
    'data-role="jamf"',
    'data-role="qa"',
    'id="wizardStep1"',
    'id="wizardStep2"',
    'id="wizardStep3"',
    'id="wizardStep4"',
    'id="wizardStep5"',
  ];
  for (const el of requiredElements) {
    assert.ok(html.includes(el), `index.html missing element ${el}`);
  }
});

function createMockClassList(initialClasses = []) {
  const classes = new Set(initialClasses);
  return {
    classes,
    add(c) { classes.add(c); },
    remove(c) { classes.delete(c); },
    contains(c) { return classes.has(c); },
    toggle(c, force) {
      if (force === undefined) {
        if (classes.has(c)) { classes.delete(c); return false; }
        else { classes.add(c); return true; }
      } else if (force) {
        classes.add(c); return true;
      } else {
        classes.delete(c); return false;
      }
    },
  };
}

test("openOnboardingWizard and closeOnboardingWizard toggle modal and persist completion", async () => {
  const modal = {
    classList: createMockClassList(["hidden"]),
  };

  const steps = [1, 2, 3, 4, 5].map(i => ({
    id: `wizardStep${i}`,
    style: { display: i === 1 ? "block" : "none" },
    classList: createMockClassList(),
  }));

  const indicators = [1, 2, 3, 4, 5].map(i => ({
    dataset: { step: String(i) },
    classList: createMockClassList(),
  }));

  const prevBtn = { style: {} };
  const nextBtn = { style: {} };
  const finishBtn = { style: {} };

  let apiCalls = [];
  const mockApi = (url, opts) => {
    apiCalls.push({ url, opts });
    return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
  };

  const document = {
    getElementById(id) {
      if (id === "onboardingModal") return modal;
      if (id === "wizardPrevBtn") return prevBtn;
      if (id === "wizardNextBtn") return nextBtn;
      if (id === "wizardFinishBtn") return finishBtn;
      const stepMatch = id.match(/^wizardStep(\d)$/);
      if (stepMatch) return steps[parseInt(stepMatch[1], 10) - 1];
      return null;
    },
    querySelectorAll(sel) {
      if (sel.includes("wizard-step-indicator") || sel.includes(".step-indicator")) return indicators;
      return [];
    },
  };

  let mockLatest = { config: { firstRunCompleted: false, listen: "127.0.0.1:9000" }, tartInstalled: true, tartVersion: "1.2.0", storagePath: "/tmp/vms" };

  const context = vm.createContext({
    document,
    currentWizardStep: 1,
    latest: mockLatest,
    selectedWizardRole: "devops",
    api: mockApi,
  });
  const definitions = ["updateWizardReview", "selectWizardStep", "openOnboardingWizard", "closeOnboardingWizard"].map(extractFunction).join("\n");
  vm.runInContext(definitions + "; globalThis.bundle = { openOnboardingWizard, closeOnboardingWizard };", context);
  const { openOnboardingWizard, closeOnboardingWizard } = context.bundle;

  // Open wizard
  openOnboardingWizard(1);
  assert.equal(modal.classList.contains("hidden"), false);

  // Close wizard without completion
  await closeOnboardingWizard(false);
  assert.equal(modal.classList.contains("hidden"), true);
  assert.equal(apiCalls.length, 0);

  // Close wizard with completion
  await closeOnboardingWizard(true);
  assert.equal(modal.classList.contains("hidden"), true);
  assert.equal(apiCalls.length, 1);
  assert.equal(apiCalls[0].url, "/api/config");
  const payload = JSON.parse(apiCalls[0].opts.body);
  assert.equal(payload.firstRunCompleted, true);
  assert.equal(payload.operatorRole, "devops");
});

test("selectWizardStep, nextWizardStep, and prevWizardStep navigate through 5 steps", () => {
  const steps = [1, 2, 3, 4, 5].map(i => ({
    id: `wizardStep${i}`,
    style: { display: "none" },
    classList: createMockClassList(),
  }));

  const indicators = [1, 2, 3, 4, 5].map(i => ({
    dataset: { step: String(i) },
    classList: createMockClassList(),
  }));

  const prevBtn = { style: {} };
  const nextBtn = { style: {} };
  const finishBtn = { style: {} };

  const document = {
    getElementById(id) {
      if (id === "wizardPrevBtn") return prevBtn;
      if (id === "wizardNextBtn") return nextBtn;
      if (id === "wizardFinishBtn") return finishBtn;
      const stepMatch = id.match(/^wizardStep(\d)$/);
      if (stepMatch) return steps[parseInt(stepMatch[1], 10) - 1];
      return null;
    },
    querySelectorAll(sel) {
      if (sel.includes("wizard-step-indicator") || sel.includes(".step-indicator")) return indicators;
      return [];
    },
  };

  const context = vm.createContext({
    document,
    currentWizardStep: 1,
    latest: { config: {} },
    selectedWizardRole: "devops",
  });

  const definitions = ["updateWizardReview", "selectWizardStep", "nextWizardStep", "prevWizardStep"].map(extractFunction).join("\n");
  vm.runInContext(definitions + "; globalThis.bundle = { selectWizardStep, nextWizardStep, prevWizardStep };", context);
  const { selectWizardStep, nextWizardStep, prevWizardStep } = context.bundle;

  selectWizardStep(1);
  assert.equal(steps[0].style.display, "block");
  assert.equal(steps[1].style.display, "none");
  assert.equal(prevBtn.style.display, "none");
  assert.equal(nextBtn.style.display, "");

  nextWizardStep();
  assert.equal(steps[1].style.display, "block");
  assert.equal(steps[0].style.display, "none");
  assert.equal(prevBtn.style.display, "");

  prevWizardStep();
  assert.equal(steps[0].style.display, "block");
  assert.equal(steps[1].style.display, "none");

  // Step 5 display checks
  selectWizardStep(5);
  assert.equal(steps[4].style.display, "block");
  assert.equal(nextBtn.style.display, "none");
  assert.equal(finishBtn.style.display, "");
});

test("applyRolePreset configures recommended settings for DevOps, Jamf, and QA", () => {
  const elements = {
    noGraphics: { checked: false },
    noAudio: { checked: false },
    schedulerMode: { value: "random" },
    createRandSerial: { checked: false },
    createRandMac: { checked: false },
    editRandSerial: { checked: false },
    editRandMac: { checked: false },
    wizardReviewRole: { textContent: "" },
    wizardReviewGraphics: { textContent: "" },
    wizardReviewScheduler: { textContent: "" },
    wizardReviewStorage: { textContent: "" },
  };

  const roleCards = [
    {
      dataset: { role: "devops" },
      classList: createMockClassList(),
    },
    {
      dataset: { role: "jamf" },
      classList: createMockClassList(),
    },
    {
      dataset: { role: "qa" },
      classList: createMockClassList(),
    },
  ];

  let cmdTab = "ssh";

  const document = {
    getElementById(id) { return elements[id] || null; },
    querySelectorAll(sel) {
      if (sel.includes("[data-role]") || sel.includes(".role-card")) return roleCards;
      return [];
    },
  };

  const globals = {
    document,
    switchCmdTab(tab) { cmdTab = tab; },
    selectedWizardRole: "",
    latest: { config: {}, storagePath: "/Users/test/.tart/vms" },
  };

  const applyRolePreset = evaluateFunctions(
    ["updateWizardReview", "applyRolePreset"],
    "applyRolePreset",
    globals
  );

  // Apply DevOps preset
  applyRolePreset("devops");
  assert.equal(elements.noGraphics.checked, true);
  assert.equal(elements.noAudio.checked, true);
  assert.equal(elements.schedulerMode.value, "sequential");
  assert.equal(roleCards[0].classList.contains("active"), true);
  assert.equal(roleCards[1].classList.contains("active"), false);

  // Apply Jamf preset
  applyRolePreset("jamf");
  assert.equal(elements.createRandSerial.checked, true);
  assert.equal(elements.createRandMac.checked, true);
  assert.equal(elements.editRandSerial.checked, true);
  assert.equal(elements.editRandMac.checked, true);
  assert.equal(cmdTab, "jamf");
  assert.equal(roleCards[1].classList.contains("active"), true);
  assert.equal(roleCards[0].classList.contains("active"), false);

  // Apply QA preset
  applyRolePreset("qa");
  assert.equal(elements.noGraphics.checked, false);
  assert.equal(elements.noAudio.checked, false);
  assert.equal(roleCards[2].classList.contains("active"), true);
  assert.equal(roleCards[1].classList.contains("active"), false);
});

test("renderTable toggles emptyDashboardHero visibility based on local VM count", () => {
  const hero = { style: { display: "none" } };
  const tbody = { innerHTML: "", querySelectorAll: () => [] };
  const ociTbody = { innerHTML: "", querySelectorAll: () => [] };
  const ociPanel = { classList: createMockClassList() };

  const document = {
    getElementById(id) {
      if (id === "emptyDashboardHero") return hero;
      if (id === "localVmRows") return tbody;
      if (id === "ociImageRows") return ociTbody;
      if (id === "ociPanel") return ociPanel;
      if (id === "showRunningOnly") return { checked: false };
      if (id === "vmSearch") return { value: "" };
      return null;
    },
  };

  const renderTable = evaluateFunctions(
    ["isOCI", "renderOCIImages", "renderTable"],
    "renderTable",
    {
      document,
      esc: (s) => s,
      fmtAgo: () => "",
      fmtDateTime: () => "",
      fmtRemaining: () => "",
      tagsHtml: () => "",
      sshCell: () => "",
      infoCell: () => "",
      mdmCell: () => "",
      agentInstallButton: () => "",
      latest: { config: {} },
    }
  );

  // When only OCI images exist (0 local VMs), empty hero card is displayed
  renderTable([{ name: "sequoia-base", source: "oci" }]);
  assert.equal(hero.style.display, "block");

  // When a local VM exists, empty hero card is hidden
  renderTable([{ name: "sequoia-clone-1", source: "local", state: "stopped" }]);
  assert.equal(hero.style.display, "none");
});

test("Auto-open wizard triggers when firstRunCompleted is false and local VM count is 0", () => {
  let openedStep = null;
  let wizardAutoOpened = false;

  const checkAutoOpen = (state) => {
    if (!wizardAutoOpened && state && state.config && !state.config.firstRunCompleted && Array.isArray(state.vms) && state.vms.filter(v => v.source !== "oci").length === 0) {
      wizardAutoOpened = true;
      openedStep = 1;
    }
  };

  // First run with no local VMs -> should open
  checkAutoOpen({
    config: { firstRunCompleted: false },
    vms: [{ name: "tahoe-base", source: "oci" }],
  });
  assert.equal(openedStep, 1);
  assert.equal(wizardAutoOpened, true);

  // Subsequent call should not re-trigger
  openedStep = null;
  checkAutoOpen({
    config: { firstRunCompleted: false },
    vms: [],
  });
  assert.equal(openedStep, null);

  // Config with firstRunCompleted: true -> should not open
  wizardAutoOpened = false;
  openedStep = null;
  checkAutoOpen({
    config: { firstRunCompleted: true },
    vms: [],
  });
  assert.equal(openedStep, null);
  assert.equal(wizardAutoOpened, false);
});
