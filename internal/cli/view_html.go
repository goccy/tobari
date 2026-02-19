package cli

import (
	"encoding/json"
	"io"
	"text/template"
)

func generateViewHTML(w io.Writer, data *viewData) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	tmpl, err := template.New("view").Parse(viewHTMLTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, map[string]string{
		"DataJSON": string(jsonData),
	})
}

const viewHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Tobari Coverage Report</title>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg:#ffffff;--bg-secondary:#f8f9fa;--bg-tertiary:#e9ecef;
  --text:#1a1a2e;--text-secondary:#495057;--text-muted:#868e96;
  --border:#dee2e6;--border-light:#e9ecef;
  --accent:#4263eb;--accent-light:#dbe4ff;
  --cov-hit-bg:#dcfce7;--cov-hit-text:#166534;
  --cov-miss-bg:#fee2e2;--cov-miss-text:#991b1b;
  --font-mono:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"Liberation Mono",monospace;
  --font-sans:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;
  --radius:6px;
  --cmp-a:#dbeafe;--cmp-a-text:#1e40af;
  --cmp-b:#fef3c7;--cmp-b-text:#92400e;
  --cmp-both:#dcfce7;--cmp-both-text:#166534;
  --cmp-none:#fee2e2;--cmp-none-text:#991b1b;
  --line-hit-no-bg:rgba(22,101,52,0.06);
  --line-miss-no-bg:rgba(153,27,27,0.06);
  --line-cmp-both-no-bg:rgba(22,101,52,0.06);
  --line-cmp-a-no-bg:rgba(30,64,175,0.06);
  --line-cmp-b-no-bg:rgba(146,64,14,0.06);
  --line-cmp-none-no-bg:rgba(153,27,27,0.06);
  --tooltip-bg:rgba(0,0,0,0.85);--tooltip-text:#fff;
  color-scheme:light dark;
}
@media(prefers-color-scheme:dark){
  :root{
    --bg:#16161e;--bg-secondary:#1e1e2a;--bg-tertiary:#2a2a3a;
    --text:#e4e4e7;--text-secondary:#a1a1aa;--text-muted:#71717a;
    --border:#3f3f52;--border-light:#2a2a3a;
    --accent:#818cf8;--accent-light:#312e81;
    --cov-hit-bg:#052e16;--cov-hit-text:#86efac;
    --cov-miss-bg:#450a0a;--cov-miss-text:#fca5a5;
    --cmp-a:#1e3a5f;--cmp-a-text:#93c5fd;
    --cmp-b:#451a03;--cmp-b-text:#fcd34d;
    --cmp-both:#052e16;--cmp-both-text:#86efac;
    --cmp-none:#450a0a;--cmp-none-text:#fca5a5;
    --line-hit-no-bg:rgba(134,239,172,0.1);
    --line-miss-no-bg:rgba(252,165,165,0.1);
    --line-cmp-both-no-bg:rgba(134,239,172,0.1);
    --line-cmp-a-no-bg:rgba(147,197,253,0.1);
    --line-cmp-b-no-bg:rgba(252,211,77,0.1);
    --line-cmp-none-no-bg:rgba(252,165,165,0.1);
    --tooltip-bg:rgba(255,255,255,0.9);--tooltip-text:#1a1a2e;
  }
}
html,body{height:100%;font-family:var(--font-sans);font-size:14px;color:var(--text);background:var(--bg)}
a{color:var(--accent);text-decoration:none}

.header{padding:12px 20px;border-bottom:1px solid var(--border);display:flex;align-items:center;gap:16px;background:var(--bg)}
.header h1{font-size:16px;font-weight:600;white-space:nowrap}
.header .badge{font-size:12px;color:var(--text-muted);background:var(--bg-secondary);padding:2px 8px;border-radius:10px}

.tabs{display:flex;border-bottom:1px solid var(--border);background:var(--bg);padding:0 20px}
.tab{padding:10px 20px;cursor:pointer;font-size:13px;font-weight:500;color:var(--text-secondary);border-bottom:2px solid transparent;transition:all .15s}
.tab:hover{color:var(--text)}
.tab.active{color:var(--accent);border-bottom-color:var(--accent)}
.tab-content{display:none;height:calc(100vh - 95px);overflow:hidden}
.tab-content.active{display:flex;flex-direction:column}

/* Coverage Tab */
.coverage-layout{display:flex;flex:1;overflow:hidden}
.test-panel{width:280px;min-width:200px;border-right:1px solid var(--border);display:flex;flex-direction:column;background:var(--bg)}
.test-panel-header{padding:10px 12px;border-bottom:1px solid var(--border-light);display:flex;flex-direction:column;gap:6px}
.test-filter{width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:var(--radius);font-size:12px;outline:none;background:var(--bg);color:var(--text)}
.test-filter:focus{border-color:var(--accent)}
.test-actions{display:flex;gap:4px;flex-wrap:wrap}
.test-actions button{flex:1;padding:4px 8px;font-size:11px;border:1px solid var(--border);border-radius:var(--radius);background:var(--bg);cursor:pointer;color:var(--text-secondary);min-width:0}
.test-actions button:hover{background:var(--bg-secondary)}
.test-tree{flex:1;overflow-y:auto;padding:4px 0}
.test-stats{padding:8px 12px;border-top:1px solid var(--border-light);font-size:11px;color:var(--text-secondary)}

.tree-node{user-select:none}
.tree-label{display:flex;align-items:center;padding:2px 8px;cursor:pointer;font-size:12px;line-height:1.6}
.tree-label:hover{background:var(--bg-secondary)}
.tree-toggle{width:16px;height:16px;display:inline-flex;align-items:center;justify-content:center;font-size:10px;color:var(--text-muted);flex-shrink:0}
.tree-toggle.has-children{cursor:pointer}
.tree-toggle.has-children:hover{color:var(--text)}
.tree-checkbox{margin:0 4px 0 0;cursor:pointer;accent-color:var(--accent)}
.tree-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1}
.tree-children{display:none}
.tree-node.expanded>.tree-children{display:block}
.tree-children .tree-label{padding-left:calc(var(--depth,0)*16px + 8px)}

.source-panel{flex:1;display:flex;flex-direction:column;overflow:hidden}
.source-header{padding:8px 12px;border-bottom:1px solid var(--border-light);display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.file-select{padding:4px 8px;border:1px solid var(--border);border-radius:var(--radius);font-size:12px;font-family:var(--font-mono);max-width:100%;outline:none;background:var(--bg);color:var(--text)}

/* Compare banner */
.compare-banner{padding:8px 12px;background:var(--bg-secondary);border-bottom:1px solid var(--border-light);font-size:12px;display:none;flex-direction:column;gap:6px}
.compare-banner.active{display:flex}
.compare-banner .cmp-label{display:inline-flex;align-items:center;gap:4px}
.compare-banner .cmp-swatch{display:inline-block;width:12px;height:12px;border-radius:2px}
.compare-banner button{padding:3px 10px;font-size:11px;border:1px solid var(--border);border-radius:var(--radius);background:var(--bg);cursor:pointer;color:var(--text-secondary)}
.compare-banner button:hover{background:var(--bg-tertiary)}
.compare-legend{display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.compare-nav{display:flex;align-items:center;gap:8px;width:100%;flex-wrap:wrap}
.compare-nav-label{font-size:11px;color:var(--text-secondary);font-weight:600}
.diff-badges{display:flex;gap:4px;flex:1;flex-wrap:wrap}
.diff-badge{padding:2px 8px;font-size:11px;border-radius:10px;background:var(--bg-tertiary);cursor:pointer;white-space:nowrap}
.diff-badge:hover{background:var(--border)}
.diff-badge.active{background:var(--accent-light);color:var(--accent)}
.diff-nav-controls{display:flex;align-items:center;gap:6px}
#diff-counter{font-size:11px;color:var(--text-secondary);min-width:60px;text-align:center}
.line-diff-focus .line-no{border-left:4px solid var(--accent) !important}

.source-code{flex:1;overflow:auto;font-family:var(--font-mono);font-size:13px;line-height:1.6}
.source-table{border-collapse:collapse;width:100%}
.source-table td{padding:0;white-space:pre;vertical-align:top}
.line-no{text-align:right;padding:0 12px 0 8px !important;color:var(--text-muted);user-select:none;width:1px;border-right:1px solid var(--border-light);font-size:12px}
.line-content{padding:0 8px !important}
.line-hit{background:var(--cov-hit-bg)}
.line-hit .line-no{color:var(--cov-hit-text);background:var(--line-hit-no-bg)}
.line-miss{background:var(--cov-miss-bg)}
.line-miss .line-no{color:var(--cov-miss-text);background:var(--line-miss-no-bg)}
.line-cmp-both{background:var(--cmp-both)}
.line-cmp-both .line-no{color:var(--cmp-both-text);background:var(--line-cmp-both-no-bg)}
.line-cmp-a{background:var(--cmp-a)}
.line-cmp-a .line-no{color:var(--cmp-a-text);background:var(--line-cmp-a-no-bg)}
.line-cmp-b{background:var(--cmp-b)}
.line-cmp-b .line-no{color:var(--cmp-b-text);background:var(--line-cmp-b-no-bg)}
.line-cmp-none{background:var(--cov-miss-bg)}
.line-cmp-none .line-no{color:var(--cov-miss-text);background:var(--line-cmp-none-no-bg)}

/* Overlap Tab */
.overlap-layout{flex:1;overflow-y:auto;padding:20px}
.overlap-section{margin-bottom:24px}
.overlap-section h2{font-size:15px;font-weight:600;margin-bottom:12px}
.matrix-container{overflow:auto;margin-bottom:16px}
.matrix-legend{display:flex;gap:16px;font-size:12px;color:var(--text-secondary);margin-bottom:16px;flex-wrap:wrap}
.matrix-legend span{display:flex;align-items:center;gap:4px}
.legend-dot{width:12px;height:12px;border-radius:50%;display:inline-block}

.overlap-filter{display:flex;align-items:center;gap:12px;margin-bottom:16px;flex-wrap:wrap}
.overlap-filter input[type="text"]{padding:5px 8px;border:1px solid var(--border);border-radius:var(--radius);font-size:12px;outline:none;width:200px;background:var(--bg);color:var(--text)}
.overlap-filter input[type="text"]:focus{border-color:var(--accent)}
.overlap-filter label{font-size:12px;color:var(--text-secondary);display:flex;align-items:center;gap:4px}
.overlap-filter input[type="number"]{width:60px;padding:4px 6px;border:1px solid var(--border);border-radius:var(--radius);font-size:12px;outline:none;text-align:center;background:var(--bg);color:var(--text)}
.overlap-filter input[type="number"]:focus{border-color:var(--accent)}
.overlap-filter-count{font-size:12px;color:var(--text-muted);margin-left:auto}

.overlap-table{width:100%;border-collapse:collapse;font-size:13px}
.overlap-table th{text-align:left;padding:8px 12px;border-bottom:2px solid var(--border);font-weight:600;color:var(--text-secondary);font-size:12px}
.overlap-table td{padding:8px 12px;border-bottom:1px solid var(--border-light)}
.overlap-table tr{cursor:pointer}
.overlap-table tbody tr:hover{background:var(--bg-secondary)}
.match-bar{display:inline-block;height:6px;border-radius:3px;margin-right:8px;vertical-align:middle}

/* Summary Tab */
.summary-layout{flex:1;overflow-y:auto;padding:20px}
.summary-stats{display:flex;gap:16px;margin-bottom:24px;flex-wrap:wrap}
.stat-card{background:var(--bg-secondary);border-radius:var(--radius);padding:16px 20px;flex:1;min-width:150px}
.stat-value{font-size:24px;font-weight:700;color:var(--text)}
.stat-label{font-size:12px;color:var(--text-secondary);margin-top:2px}
.summary-section{margin-bottom:24px}
.summary-section h2{font-size:15px;font-weight:600;margin-bottom:12px}
.summary-table{width:100%;border-collapse:collapse;font-size:13px}
.summary-table th{text-align:left;padding:8px 12px;border-bottom:2px solid var(--border);font-weight:600;color:var(--text-secondary);font-size:12px}
.summary-table td{padding:8px 12px;border-bottom:1px solid var(--border-light)}
.cov-bar{display:inline-block;width:60px;height:6px;border-radius:3px;background:var(--bg-tertiary);margin-right:6px;vertical-align:middle}
.cov-bar-fill{display:block;height:100%;border-radius:3px}

.svg-tooltip{position:fixed;background:var(--tooltip-bg);color:var(--tooltip-text);padding:6px 10px;border-radius:4px;font-size:12px;pointer-events:none;z-index:1000;white-space:nowrap;display:none}
</style>
</head>
<body>

<div class="header">
  <h1>Tobari Coverage Report</h1>
  <span class="badge" id="header-badge"></span>
</div>

<div class="tabs">
  <div class="tab active" data-tab="coverage">Coverage</div>
  <div class="tab" data-tab="overlap">Overlap Analysis</div>
  <div class="tab" data-tab="summary">Summary</div>
</div>

<div class="tab-content active" id="tab-coverage">
  <div class="coverage-layout">
    <div class="test-panel">
      <div class="test-panel-header">
        <input type="text" class="test-filter" id="test-filter" placeholder="Filter tests...">
        <div class="test-actions">
          <button id="btn-select-all">Select All</button>
          <button id="btn-deselect-all">Deselect All</button>
        </div>
      </div>
      <div class="test-tree" id="test-tree"></div>
      <div class="test-stats" id="test-stats"></div>
    </div>
    <div class="source-panel">
      <div class="compare-banner" id="compare-banner">
        <div class="compare-legend">
          <span class="cmp-label"><span class="cmp-swatch" style="background:var(--cmp-both)"></span> Both</span>
          <span class="cmp-label"><span class="cmp-swatch" style="background:var(--cmp-a)"></span> <span id="cmp-name-a">Test A</span> only</span>
          <span class="cmp-label"><span class="cmp-swatch" style="background:var(--cmp-b)"></span> <span id="cmp-name-b">Test B</span> only</span>
          <span class="cmp-label"><span class="cmp-swatch" style="background:var(--cov-miss-bg)"></span> Neither</span>
        </div>
        <div class="compare-nav">
          <span class="compare-nav-label">Diffs:</span>
          <div class="diff-badges" id="diff-badges"></div>
          <div class="diff-nav-controls">
            <button id="btn-prev-diff">Prev</button>
            <span id="diff-counter">0 / 0</span>
            <button id="btn-next-diff">Next</button>
          </div>
          <button id="btn-exit-compare">Exit Compare</button>
        </div>
      </div>
      <div class="source-header">
        <select class="file-select" id="file-select"></select>
      </div>
      <div class="source-code" id="source-code"></div>
    </div>
  </div>
</div>

<div class="tab-content" id="tab-overlap">
  <div class="overlap-layout">
    <div class="overlap-section">
      <h2>Coverage Overlap Matrix</h2>
      <div class="matrix-container" id="overlap-matrix"></div>
      <div class="matrix-legend">
        <span><span class="legend-dot" style="background:#dc2626"></span> 90-100%</span>
        <span><span class="legend-dot" style="background:#ea580c"></span> 70-90%</span>
        <span><span class="legend-dot" style="background:#f59e0b"></span> 50-70%</span>
        <span><span class="legend-dot" style="background:#a3e635"></span> 30-50%</span>
        <span><span class="legend-dot" style="background:#e5e7eb"></span> &lt;30%</span>
      </div>
    </div>
    <div class="overlap-section">
      <h2>Overlap Rankings</h2>
      <div class="overlap-filter">
        <input type="text" id="overlap-test-filter" placeholder="Filter by test name...">
        <label>Min: <input type="number" id="overlap-min-rate" min="0" max="100" value="0" step="1">%</label>
        <label>Max: <input type="number" id="overlap-max-rate" min="0" max="100" value="100" step="1">%</label>
        <span class="overlap-filter-count" id="overlap-filter-count"></span>
      </div>
      <table class="overlap-table" id="overlap-rankings">
        <thead><tr><th>#</th><th>Test A</th><th>Test B</th><th>Match Rate</th><th>Common / Total</th></tr></thead>
        <tbody></tbody>
      </table>
    </div>
  </div>
</div>

<div class="tab-content" id="tab-summary">
  <div class="summary-layout">
    <div class="summary-stats" id="summary-stats"></div>
    <div class="summary-section">
      <h2>Per-File Coverage</h2>
      <table class="summary-table" id="file-coverage-table">
        <thead><tr><th>File</th><th>Statements</th><th>Covered</th><th>Coverage</th></tr></thead>
        <tbody></tbody>
      </table>
    </div>
    <div class="summary-section">
      <h2>Per-Test Coverage (top-level)</h2>
      <table class="summary-table" id="test-coverage-table">
        <thead><tr><th>Test</th><th>Covered Lines</th><th>Total Lines</th><th>Coverage</th></tr></thead>
        <tbody></tbody>
      </table>
    </div>
  </div>
</div>

<div class="svg-tooltip" id="svg-tooltip"></div>

<script>
const DATA = {{.DataJSON}};

// ========== State ==========
const state = {
  selectedTests: new Set(),
  currentFile: 0,
  compareMode: null, // null or { testA: string, testB: string }
};

// Precompute instrumented lines per file from DATA.instrLines (includes Count=0 entries)
const instrLineSets = {};
if (DATA.instrLines) {
  for (const [fi, lines] of Object.entries(DATA.instrLines)) {
    instrLineSets[fi] = new Set(lines);
  }
}

// ========== Init ==========
(function init() {
  DATA.tests.forEach(t => state.selectedTests.add(t.n));

  setupTabs();
  renderTestTree();
  renderFileSelect();
  renderSourceCode();
  updateStats();
  renderOverlapMatrix();
  renderOverlapRankings();
  setupOverlapFilters();
  renderSummary();
  updateHeaderBadge();

  document.getElementById('btn-exit-compare').addEventListener('click', exitCompareMode);
  document.getElementById('btn-prev-diff').addEventListener('click', () => navigateDiff(-1));
  document.getElementById('btn-next-diff').addEventListener('click', () => navigateDiff(1));
})();

// ========== Tabs ==========
function setupTabs() {
  document.querySelectorAll('.tab').forEach(tab => {
    tab.addEventListener('click', () => {
      document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
      document.querySelectorAll('.tab-content').forEach(tc => tc.classList.remove('active'));
      tab.classList.add('active');
      document.getElementById('tab-' + tab.dataset.tab).classList.add('active');
    });
  });
}

// ========== Header Badge ==========
function updateHeaderBadge() {
  const merged = getMergedCoverage();
  let totalInstr = 0, totalCov = 0;
  for (const [fi, instrSet] of Object.entries(instrLineSets)) {
    totalInstr += instrSet.size;
    const covSet = merged[fi] || new Set();
    for (const l of instrSet) { if (covSet.has(l)) totalCov++; }
  }
  const pct = totalInstr > 0 ? (totalCov / totalInstr * 100).toFixed(1) : '0.0';
  document.getElementById('header-badge').textContent = pct + '% coverage';
}

// ========== Test Tree ==========
function renderTestTree() {
  const container = document.getElementById('test-tree');
  container.innerHTML = '';
  renderNodes(DATA.testTree, container, 0);

  document.getElementById('btn-select-all').addEventListener('click', () => {
    DATA.tests.forEach(t => state.selectedTests.add(t.n));
    exitCompareMode();
    updateAllCheckboxes();
    onSelectionChange();
  });
  document.getElementById('btn-deselect-all').addEventListener('click', () => {
    state.selectedTests.clear();
    exitCompareMode();
    updateAllCheckboxes();
    onSelectionChange();
  });
  document.getElementById('test-filter').addEventListener('input', (e) => {
    filterTree(e.target.value.toLowerCase());
  });
}

function renderNodes(nodes, container, depth) {
  nodes.forEach(node => {
    const div = document.createElement('div');
    div.className = 'tree-node';
    div.dataset.fullname = node.fullName;

    const label = document.createElement('div');
    label.className = 'tree-label';
    label.style.setProperty('--depth', depth);

    const toggle = document.createElement('span');
    toggle.className = 'tree-toggle';
    if (node.children && node.children.length > 0) {
      toggle.className += ' has-children';
      toggle.textContent = '\u25B8';
      toggle.addEventListener('click', (e) => {
        e.stopPropagation();
        div.classList.toggle('expanded');
        toggle.textContent = div.classList.contains('expanded') ? '\u25BE' : '\u25B8';
      });
    }

    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.className = 'tree-checkbox';
    cb.checked = state.selectedTests.has(node.fullName);
    cb.addEventListener('change', () => {
      toggleNode(node, cb.checked);
      exitCompareMode();
      onSelectionChange();
    });

    const nameSpan = document.createElement('span');
    nameSpan.className = 'tree-name';
    nameSpan.textContent = node.name;
    nameSpan.title = node.fullName;

    label.appendChild(toggle);
    label.appendChild(cb);
    label.appendChild(nameSpan);
    div.appendChild(label);

    if (node.children && node.children.length > 0) {
      const childContainer = document.createElement('div');
      childContainer.className = 'tree-children';
      renderNodes(node.children, childContainer, depth + 1);
      div.appendChild(childContainer);
    }

    container.appendChild(div);
  });
}

function toggleNode(node, checked) {
  if (checked) { state.selectedTests.add(node.fullName); }
  else { state.selectedTests.delete(node.fullName); }
  if (node.children) {
    node.children.forEach(child => toggleNode(child, checked));
  }
}

function updateAllCheckboxes() {
  document.querySelectorAll('.tree-node').forEach(div => {
    const cb = div.querySelector(':scope > .tree-label > .tree-checkbox');
    if (cb) cb.checked = state.selectedTests.has(div.dataset.fullname);
  });
}

function filterTree(query) {
  document.querySelectorAll('.tree-node').forEach(div => {
    const name = div.dataset.fullname.toLowerCase();
    div.style.display = (!query || name.includes(query)) ? '' : 'none';
  });
  if (query) {
    document.querySelectorAll('.tree-node').forEach(div => {
      if (div.style.display === 'none') {
        const visibleChild = div.querySelector('.tree-node[style=""]');
        if (visibleChild) {
          div.style.display = '';
          div.classList.add('expanded');
          const toggle = div.querySelector(':scope > .tree-label > .tree-toggle');
          if (toggle) toggle.textContent = '\u25BE';
        }
      }
    });
  }
}

// ========== Selection Change ==========
let selectionTimeout = null;
function onSelectionChange() {
  clearTimeout(selectionTimeout);
  selectionTimeout = setTimeout(() => {
    updateAllCheckboxes();
    renderSourceCode();
    updateStats();
    updateHeaderBadge();
  }, 50);
}

// ========== Merged Coverage ==========
function getMergedCoverage() {
  const merged = {};
  DATA.tests.forEach(t => {
    if (!state.selectedTests.has(t.n) || !t.c) return;
    for (const [fi, lines] of Object.entries(t.c)) {
      if (!merged[fi]) merged[fi] = new Set();
      lines.forEach(l => merged[fi].add(l));
    }
  });
  return merged;
}

// Get coverage for a single top-level test group (merging all subtests)
function getTestGroupCoverage(prefix) {
  const merged = {};
  DATA.tests.forEach(t => {
    if (!t.c) return;
    if (t.n !== prefix && !t.n.startsWith(prefix + '/')) return;
    for (const [fi, lines] of Object.entries(t.c)) {
      if (!merged[fi]) merged[fi] = new Set();
      lines.forEach(l => merged[fi].add(l));
    }
  });
  return merged;
}

// ========== Compare Mode ==========
function buildDiffList(testA, testB) {
  const covA = getTestGroupCoverage(testA);
  const covB = getTestGroupCoverage(testB);
  const diffs = [];
  DATA.files.forEach((f, fi) => {
    const instrSet = instrLineSets[fi] || new Set();
    const setA = covA[fi] || new Set();
    const setB = covB[fi] || new Set();
    const sortedLines = Array.from(instrSet).sort((a, b) => a - b);
    for (const lineNo of sortedLines) {
      const inA = setA.has(lineNo);
      const inB = setB.has(lineNo);
      if ((inA && !inB) || (!inA && inB)) {
        diffs.push({ fileIndex: fi, lineNo: lineNo });
      }
    }
  });
  return diffs;
}

function renderDiffBadges() {
  const container = document.getElementById('diff-badges');
  container.innerHTML = '';
  if (!state.compareMode) return;
  const counts = {};
  for (const d of state.compareMode.allDiffs) {
    counts[d.fileIndex] = (counts[d.fileIndex] || 0) + 1;
  }
  DATA.files.forEach((f, fi) => {
    if (!counts[fi]) return;
    const badge = document.createElement('span');
    badge.className = 'diff-badge' + (fi === state.currentFile ? ' active' : '');
    badge.textContent = f.short + '(' + counts[fi] + ')';
    badge.addEventListener('click', () => {
      const idx = state.compareMode.allDiffs.findIndex(d => d.fileIndex === fi);
      if (idx >= 0) {
        state.compareMode.diffIndex = idx;
        state.currentFile = fi;
        document.getElementById('file-select').value = fi;
        renderSourceCode();
        scrollToDiffLine(state.compareMode.allDiffs[idx].lineNo);
        updateDiffCounter();
        updateDiffBadgeActive();
      }
    });
    container.appendChild(badge);
  });
}

function updateDiffBadgeActive() {
  const badges = document.querySelectorAll('.diff-badge');
  badges.forEach(b => b.classList.remove('active'));
  const activeFi = state.currentFile;
  badges.forEach(b => {
    const text = b.textContent;
    const file = DATA.files[activeFi];
    if (file && text.startsWith(file.short + '(')) {
      b.classList.add('active');
    }
  });
}

function updateDiffCounter() {
  const el = document.getElementById('diff-counter');
  if (!state.compareMode || state.compareMode.allDiffs.length === 0) {
    el.textContent = '0 / 0';
    return;
  }
  el.textContent = (state.compareMode.diffIndex + 1) + ' / ' + state.compareMode.allDiffs.length;
}

function navigateDiff(direction) {
  const cmp = state.compareMode;
  if (!cmp || cmp.allDiffs.length === 0) return;
  cmp.diffIndex = (cmp.diffIndex + direction + cmp.allDiffs.length) % cmp.allDiffs.length;
  const diff = cmp.allDiffs[cmp.diffIndex];
  if (state.currentFile !== diff.fileIndex) {
    state.currentFile = diff.fileIndex;
    document.getElementById('file-select').value = diff.fileIndex;
    renderSourceCode();
    updateDiffBadgeActive();
  }
  scrollToDiffLine(diff.lineNo);
  updateDiffCounter();
}

function scrollToDiffLine(lineNo) {
  // Remove previous focus
  const prev = document.querySelector('.line-diff-focus');
  if (prev) prev.classList.remove('line-diff-focus');
  // Find and scroll to the target row
  const rows = document.querySelectorAll('.source-table tr');
  const row = rows[lineNo - 1];
  if (row) {
    row.classList.add('line-diff-focus');
    row.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }
}

function enterCompareMode(testA, testB) {
  const allDiffs = buildDiffList(testA, testB);
  state.compareMode = { testA, testB, allDiffs, diffIndex: 0 };
  document.getElementById('cmp-name-a').textContent = testA;
  document.getElementById('cmp-name-b').textContent = testB;
  document.getElementById('compare-banner').classList.add('active');
  renderDiffBadges();
  // Jump to the first diff
  if (allDiffs.length > 0) {
    const first = allDiffs[0];
    if (state.currentFile !== first.fileIndex) {
      state.currentFile = first.fileIndex;
      document.getElementById('file-select').value = first.fileIndex;
    }
    renderSourceCode();
    updateDiffCounter();
    setTimeout(() => scrollToDiffLine(first.lineNo), 50);
  } else {
    renderSourceCode();
    updateDiffCounter();
  }
}

function exitCompareMode() {
  state.compareMode = null;
  document.getElementById('compare-banner').classList.remove('active');
  document.getElementById('diff-badges').innerHTML = '';
  document.getElementById('diff-counter').textContent = '0 / 0';
  const prev = document.querySelector('.line-diff-focus');
  if (prev) prev.classList.remove('line-diff-focus');
  renderSourceCode();
}

// ========== File Select ==========
function renderFileSelect() {
  const sel = document.getElementById('file-select');
  sel.innerHTML = '';
  DATA.files.forEach((f, i) => {
    const opt = document.createElement('option');
    opt.value = i;
    opt.textContent = f.short;
    sel.appendChild(opt);
  });
  sel.addEventListener('change', () => {
    state.currentFile = parseInt(sel.value);
    renderSourceCode();
  });
}

// ========== Source Code ==========
function renderSourceCode() {
  const container = document.getElementById('source-code');
  const fi = state.currentFile;
  const file = DATA.files[fi];
  if (!file) { container.innerHTML = ''; return; }

  const instrSet = instrLineSets[fi] || new Set();

  // Update file select to show coverage %
  const merged = getMergedCoverage();
  const sel = document.getElementById('file-select');
  DATA.files.forEach((f, idx) => {
    const instr = instrLineSets[idx] || new Set();
    const cov = merged[idx] || new Set();
    let covered = 0;
    for (const l of instr) { if (cov.has(l)) covered++; }
    const pct = instr.size > 0 ? (covered / instr.size * 100).toFixed(1) : '-';
    sel.options[idx].textContent = f.short + ' (' + pct + '%)';
  });

  let html = '<table class="source-table">';

  if (state.compareMode) {
    // Compare mode: show coverage from testA vs testB with distinct colors
    const covA = getTestGroupCoverage(state.compareMode.testA);
    const covB = getTestGroupCoverage(state.compareMode.testB);
    const setA = covA[fi] || new Set();
    const setB = covB[fi] || new Set();

    file.lines.forEach((line, idx) => {
      const lineNo = idx + 1;
      let cls = '';
      if (instrSet.has(lineNo)) {
        const inA = setA.has(lineNo);
        const inB = setB.has(lineNo);
        if (inA && inB) cls = ' class="line-cmp-both"';
        else if (inA) cls = ' class="line-cmp-a"';
        else if (inB) cls = ' class="line-cmp-b"';
        else cls = ' class="line-cmp-none"';
      }
      const escaped = escapeHTML(line);
      html += '<tr' + cls + '><td class="line-no">' + lineNo + '</td><td class="line-content">' + (escaped || ' ') + '</td></tr>';
    });
  } else {
    // Normal mode: merged coverage
    const coveredLines = merged[fi] || new Set();
    file.lines.forEach((line, idx) => {
      const lineNo = idx + 1;
      let cls = '';
      if (instrSet.has(lineNo)) {
        cls = coveredLines.has(lineNo) ? ' class="line-hit"' : ' class="line-miss"';
      }
      const escaped = escapeHTML(line);
      html += '<tr' + cls + '><td class="line-no">' + lineNo + '</td><td class="line-content">' + (escaped || ' ') + '</td></tr>';
    });
  }

  html += '</table>';
  container.innerHTML = html;
}

function escapeHTML(s) {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

// ========== Stats ==========
function updateStats() {
  const merged = getMergedCoverage();
  let totalInstr = 0, totalCov = 0;
  for (const [fi, instrSet] of Object.entries(instrLineSets)) {
    totalInstr += instrSet.size;
    const cov = merged[fi] || new Set();
    for (const l of instrSet) { if (cov.has(l)) totalCov++; }
  }
  const pct = totalInstr > 0 ? (totalCov / totalInstr * 100).toFixed(1) : '0.0';
  document.getElementById('test-stats').innerHTML =
    'Selected: ' + state.selectedTests.size + ' / ' + DATA.tests.length +
    '<br>Coverage: ' + pct + '%';
}

// ========== Overlap Matrix (SVG) ==========
function renderOverlapMatrix() {
  const container = document.getElementById('overlap-matrix');
  if (!DATA.overlaps || DATA.overlaps.length === 0) {
    container.innerHTML = '<p style="color:var(--text-muted);font-size:13px">No overlap data available.</p>';
    return;
  }

  const nameSet = new Set();
  DATA.overlaps.forEach(o => { nameSet.add(o.testA); nameSet.add(o.testB); });
  const names = Array.from(nameSet).sort();
  const nameIndex = {};
  names.forEach((n, i) => { nameIndex[n] = i; });

  const overlapMap = {};
  DATA.overlaps.forEach(o => {
    overlapMap[o.testA + '|' + o.testB] = o.matchRate;
    overlapMap[o.testB + '|' + o.testA] = o.matchRate;
  });

  const n = names.length;
  const cellSize = Math.min(Math.max(8, Math.floor(500 / n)), 20);
  const labelWidth = 120;
  const labelHeight = 120;
  const svgW = labelWidth + n * cellSize + 10;
  const svgH = labelHeight + n * cellSize + 10;

  let svg = '<svg xmlns="http://www.w3.org/2000/svg" width="' + svgW + '" height="' + svgH + '" style="font-family:var(--font-sans);font-size:10px">';

  names.forEach((name, i) => {
    const y = labelHeight + i * cellSize + cellSize / 2;
    const short = name.length > 16 ? name.substring(0, 15) + '\u2026' : name;
    svg += '<text x="' + (labelWidth - 4) + '" y="' + (y + 3) + '" text-anchor="end" fill="var(--text-secondary)">' + escapeHTML(short) + '</text>';
  });
  names.forEach((name, i) => {
    const x = labelWidth + i * cellSize + cellSize / 2;
    const short = name.length > 16 ? name.substring(0, 15) + '\u2026' : name;
    svg += '<text x="' + x + '" y="' + (labelHeight - 4) + '" text-anchor="start" transform="rotate(-60,' + x + ',' + (labelHeight - 4) + ')" fill="var(--text-secondary)">' + escapeHTML(short) + '</text>';
  });

  names.forEach((nameA, i) => {
    names.forEach((nameB, j) => {
      if (i === j) return;
      const key = nameA + '|' + nameB;
      const rate = overlapMap[key] || 0;
      if (rate < 0.01) return;
      const x = labelWidth + j * cellSize;
      const y = labelHeight + i * cellSize;
      const color = rateColor(rate);
      svg += '<rect x="' + x + '" y="' + y + '" width="' + cellSize + '" height="' + cellSize + '" fill="' + color + '" rx="1"';
      svg += ' data-testa="' + escapeHTML(nameA) + '" data-testb="' + escapeHTML(nameB) + '"';
      svg += ' data-tooltip="' + escapeHTML(nameA) + ' \u2194 ' + escapeHTML(nameB) + ': ' + (rate * 100).toFixed(1) + '%"';
      svg += ' style="cursor:pointer"';
      svg += ' onmouseenter="showTooltip(event)" onmouseleave="hideTooltip()"';
      svg += ' onclick="onMatrixCellClick(this)"';
      svg += '/>';
    });
  });

  svg += '</svg>';
  container.innerHTML = svg;
}

function rateColor(rate) {
  if (rate >= 0.9) return '#dc2626';
  if (rate >= 0.7) return '#ea580c';
  if (rate >= 0.5) return '#f59e0b';
  if (rate >= 0.3) return '#a3e635';
  return '#e5e7eb';
}

function onMatrixCellClick(el) {
  const testA = el.getAttribute('data-testa');
  const testB = el.getAttribute('data-testb');
  startCompare(testA, testB);
}

// ========== Overlap Rankings ==========
function renderOverlapRankings() {
  const tbody = document.querySelector('#overlap-rankings tbody');
  tbody.innerHTML = '';
  if (!DATA.overlaps) return;

  const testQuery = (document.getElementById('overlap-test-filter').value || '').toLowerCase();
  const minRate = parseFloat(document.getElementById('overlap-min-rate').value) || 0;
  const maxRate = parseFloat(document.getElementById('overlap-max-rate').value);
  const maxRateVal = isNaN(maxRate) ? 100 : maxRate;

  let rank = 0;
  let shown = 0;
  DATA.overlaps.forEach((o) => {
    const pctVal = o.matchRate * 100;
    if (pctVal < minRate || pctVal > maxRateVal) return;
    if (testQuery && !o.testA.toLowerCase().includes(testQuery) && !o.testB.toLowerCase().includes(testQuery)) return;

    shown++;
    rank++;
    const tr = document.createElement('tr');
    const pct = pctVal.toFixed(1);
    const barW = Math.max(2, o.matchRate * 80);
    tr.innerHTML =
      '<td>' + rank + '</td>' +
      '<td>' + escapeHTML(o.testA) + '</td>' +
      '<td>' + escapeHTML(o.testB) + '</td>' +
      '<td><span class="match-bar" style="width:' + barW + 'px;background:' + rateColor(o.matchRate) + '"></span>' + pct + '%</td>' +
      '<td>' + o.common + ' / ' + o.total + '</td>';
    tr.addEventListener('click', () => { startCompare(o.testA, o.testB); });
    tbody.appendChild(tr);
  });

  document.getElementById('overlap-filter-count').textContent = shown + ' / ' + DATA.overlaps.length + ' pairs';
}

function setupOverlapFilters() {
  const testFilter = document.getElementById('overlap-test-filter');
  const minRate = document.getElementById('overlap-min-rate');
  const maxRate = document.getElementById('overlap-max-rate');
  let filterTimeout = null;
  const onFilterChange = () => {
    clearTimeout(filterTimeout);
    filterTimeout = setTimeout(renderOverlapRankings, 150);
  };
  testFilter.addEventListener('input', onFilterChange);
  minRate.addEventListener('input', onFilterChange);
  maxRate.addEventListener('input', onFilterChange);
}

function startCompare(testA, testB) {
  // Select both test groups and enter compare mode
  state.selectedTests.clear();
  selectTestAndChildren(testA);
  selectTestAndChildren(testB);
  updateAllCheckboxes();
  enterCompareMode(testA, testB);
  updateStats();
  updateHeaderBadge();
  document.querySelector('.tab[data-tab="coverage"]').click();
}

function selectTestAndChildren(prefix) {
  DATA.tests.forEach(t => {
    if (t.n === prefix || t.n.startsWith(prefix + '/')) {
      state.selectedTests.add(t.n);
    }
  });
}

// ========== Summary ==========
function renderSummary() {
  const allMerged = {};
  DATA.tests.forEach(t => {
    if (!t.c) return;
    for (const [fi, lines] of Object.entries(t.c)) {
      if (!allMerged[fi]) allMerged[fi] = new Set();
      lines.forEach(l => allMerged[fi].add(l));
    }
  });

  let totalInstr = 0, totalCov = 0;
  for (const [fi, instrSet] of Object.entries(instrLineSets)) {
    totalInstr += instrSet.size;
    const cov = allMerged[fi] || new Set();
    for (const l of instrSet) { if (cov.has(l)) totalCov++; }
  }

  const pct = totalInstr > 0 ? (totalCov / totalInstr * 100).toFixed(1) : '0.0';
  document.getElementById('summary-stats').innerHTML =
    '<div class="stat-card"><div class="stat-value">' + pct + '%</div><div class="stat-label">Total Coverage</div></div>' +
    '<div class="stat-card"><div class="stat-value">' + DATA.tests.length + '</div><div class="stat-label">Tests</div></div>' +
    '<div class="stat-card"><div class="stat-value">' + DATA.files.length + '</div><div class="stat-label">Files</div></div>' +
    '<div class="stat-card"><div class="stat-value">' + totalCov + ' / ' + totalInstr + '</div><div class="stat-label">Lines Covered</div></div>';

  // Per-file coverage
  const fileBody = document.querySelector('#file-coverage-table tbody');
  fileBody.innerHTML = '';
  const fileRows = [];
  DATA.files.forEach((f, fi) => {
    const instrSet = instrLineSets[fi] || new Set();
    const cov = allMerged[fi] || new Set();
    let covered = 0;
    for (const l of instrSet) { if (cov.has(l)) covered++; }
    const filePct = instrSet.size > 0 ? covered / instrSet.size : 0;
    fileRows.push({ name: f.short, total: instrSet.size, covered: covered, pct: filePct });
  });
  fileRows.sort((a, b) => a.pct - b.pct);
  fileRows.forEach(r => {
    const tr = document.createElement('tr');
    tr.innerHTML =
      '<td>' + escapeHTML(r.name) + '</td>' +
      '<td>' + r.total + '</td>' +
      '<td>' + r.covered + '</td>' +
      '<td>' + covBarHTML(r.pct) + ' ' + (r.pct * 100).toFixed(1) + '%</td>';
    fileBody.appendChild(tr);
  });

  // Per-test coverage (top-level)
  const testBody = document.querySelector('#test-coverage-table tbody');
  testBody.innerHTML = '';
  const topLevelMap = {};
  DATA.tests.forEach(t => {
    if (!t.c) return;
    const topName = t.n.split('/')[0];
    if (!topLevelMap[topName]) topLevelMap[topName] = new Set();
    for (const [fi, lines] of Object.entries(t.c)) {
      lines.forEach(l => topLevelMap[topName].add(fi + ':' + l));
    }
  });

  const testRows = [];
  for (const [name, covSet] of Object.entries(topLevelMap)) {
    let covered = 0;
    for (const key of covSet) {
      const sep = key.indexOf(':');
      const fi = key.substring(0, sep);
      const line = parseInt(key.substring(sep + 1));
      const instrSet = instrLineSets[fi];
      if (instrSet && instrSet.has(line)) covered++;
    }
    testRows.push({ name: name, covered: covered, total: totalInstr, pct: totalInstr > 0 ? covered / totalInstr : 0 });
  }
  testRows.sort((a, b) => b.pct - a.pct);
  testRows.forEach(r => {
    const tr = document.createElement('tr');
    tr.innerHTML =
      '<td>' + escapeHTML(r.name) + '</td>' +
      '<td>' + r.covered + '</td>' +
      '<td>' + r.total + '</td>' +
      '<td>' + covBarHTML(r.pct) + ' ' + (r.pct * 100).toFixed(1) + '%</td>';
    testBody.appendChild(tr);
  });
}

function covBarHTML(pct) {
  const color = pct >= 0.8 ? '#16a34a' : pct >= 0.5 ? '#f59e0b' : '#dc2626';
  return '<span class="cov-bar"><span class="cov-bar-fill" style="width:' + (pct * 100) + '%;background:' + color + '"></span></span>';
}

// ========== SVG Tooltip ==========
function showTooltip(e) {
  const tip = document.getElementById('svg-tooltip');
  tip.textContent = e.target.getAttribute('data-tooltip');
  tip.style.display = 'block';
  tip.style.left = (e.clientX + 10) + 'px';
  tip.style.top = (e.clientY - 30) + 'px';
}
function hideTooltip() {
  document.getElementById('svg-tooltip').style.display = 'none';
}
</script>
</body>
</html>`
