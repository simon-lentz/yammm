// Reads {"diagrams": [text, ...]} as JSON on stdin and writes, as JSON on
// stdout, what each pinned Mermaid release parses from each diagram text:
//
//	{"versions": [{"package": "mermaid10", "version": "10.1.0",
//	  "results": [{"error": "", "type": "classDiagram", "direction": "TB",
//	    "classes": [{"id": "A", "label": "A", "rawLabel": "A"}],
//	    "relations": [{"from": "A", "to": "B", "kind": "association",
//	      "arrow": "-->", "label": "X (one)", "rawLabel": "X (one)"}]}]}]}
//
// Each text takes the path Mermaid's render takes to its parser, so a label
// holds the entity placeholders render substitutes for "#name;" codes.
// "label" is what a browser shows of the label's text: the placeholders turned
// back into HTML entities as render does, and the entities decoded. It is the
// parser's reading; how Mermaid 11 renders a label's Markdown is not read.
import { JSDOM } from 'jsdom';
import fs from 'node:fs';

const dom = new JSDOM('<!DOCTYPE html><body></body>', { pretendToBeVisual: true });
globalThis.window = dom.window;
globalThis.document = dom.window.document;
for (const k of ['Element', 'HTMLElement', 'SVGElement', 'Node', 'DOMParser', 'navigator', 'getComputedStyle', 'MutationObserver', 'XMLSerializer']) {
  if (!(k in globalThis)) {
    try {
      globalThis[k] = dom.window[k];
    } catch {
      // navigator is a getter on some Node releases; the global one serves.
    }
  }
}

// Mermaid 10.1.0's render applies encodeEntities before getDiagramFromText,
// and its encodeEntities is not exported, so this is a copy of it. Mermaid
// 11's and 12's getDiagramFromText apply their own copy, which is
// byte-identical, so only the 10.x path calls this one.
function encodeEntities(text) {
  let txt = text;
  txt = txt.replace(/style.*:\S*#.*;/g, (s) => s.substring(0, s.length - 1));
  txt = txt.replace(/classDef.*:\S*#.*;/g, (s) => s.substring(0, s.length - 1));
  txt = txt.replace(/#\w+;/g, (s) => {
    const inner = s.substring(1, s.length - 1);
    return /^\+?\d+$/.test(inner) ? 'ﬂ\xB0\xB0' + inner + '\xB6\xDF' : 'ﬂ\xB0' + inner + '\xB6\xDF';
  });
  return txt;
}

// Render's decodeEntities, applied to the SVG it emits.
function decodeEntities(text) {
  return text.replace(/ﬂ°°/g, '&#').replace(/ﬂ°/g, '&').replace(/¶ß/g, ';');
}

// What a browser shows for a label render writes as HTML.
function shown(raw) {
  const el = document.createElement('div');
  el.innerHTML = decodeEntities(raw ?? '');
  return el.textContent;
}

// Mermaid's relationType codes; "none" is a line end with no marker.
const leftMarker = { 0: 'o', 1: '<|', 2: '*', 3: '<', 4: '()', none: '' };
const rightMarker = { 0: 'o', 1: '|>', 2: '*', 3: '>', 4: '()', none: '' };

function relationOf(r) {
  const { type1, type2, lineType } = r.relation;
  const arrow = (leftMarker[type1] ?? `?${type1}`) + (lineType === 1 ? '..' : '--') + (rightMarker[type2] ?? `?${type2}`);
  const kind = { '<|--': 'inheritance', '-->': 'association', '*--': 'composition' }[arrow] ?? 'other';
  return { from: r.id1, to: r.id2, kind, arrow, label: shown(r.title ?? ''), rawLabel: r.title ?? '' };
}

const MAX_TEXT = 50000; // render's default maxTextSize in every pinned release

async function load(pkg) {
  const { default: mermaid } = await import(pkg);
  mermaid.initialize({ startOnLoad: false });
  const version = JSON.parse(fs.readFileSync(new URL(`./node_modules/${pkg}/package.json`, import.meta.url))).version;
  return { pkg, version, mermaid };
}

async function parseOne({ pkg, mermaid }, text) {
  if (text.length > MAX_TEXT) {
    return { error: `render replaces a text of ${text.length} characters with its size message` };
  }
  let diagram;
  try {
    const input = pkg === 'mermaid10' ? encodeEntities(text.replace(/\r\n?/g, '\n')) : text;
    diagram = await mermaid.mermaidAPI.getDiagramFromText(input);
  } catch (e) {
    return { error: String(e?.message ?? e) };
  }
  const db = diagram.db;
  const classes = db.getClasses();
  const list = classes instanceof Map ? [...classes.values()] : Object.values(classes);
  const result = {
    error: '',
    type: diagram.type,
    direction: db.getDirection ? db.getDirection() : null,
    classes: list.map((c) => ({ id: c.id, label: shown(c.label), rawLabel: c.label })),
    relations: db.getRelations().map(relationOf),
  };
  // Mermaid 10.1.0 keeps the direction in module state that clear() leaves
  // alone; a browser page starts every diagram at TB, so the next one does.
  if (pkg === 'mermaid10') db.setDirection('TB');
  return result;
}

const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const versions = [];
for (const pkg of ['mermaid10', 'mermaid11', 'mermaid12']) {
  const m = await load(pkg);
  const results = [];
  for (const text of input.diagrams) results.push(await parseOne(m, text));
  versions.push({ package: pkg, version: m.version, results });
}
process.stdout.write(JSON.stringify({ versions }) + '\n');
