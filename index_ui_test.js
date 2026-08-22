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
