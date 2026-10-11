// 面板前端冒烟测试（tools/panel_smoke.js）
//
// ## 为什么需要它
//
// 面板是 embed 进二进制的静态资源，**没有构建步骤**，也没有前端测试。
// 2026-10-12 出过一次事故：新增的初始化代码放在了 bind() 里读取
// `state.growth_supported`，而 bind() 跑在 `loadState()` **之前** ——
// 此刻 state 还是 null，抛出的 TypeError 中断了整个 bind()，
// 连带 init() 走不到 await loadState()。
//
// 现象是「账号列表空、所有按钮点不了」，而后端 /api/state 完全正常
// （7 个账号、字段齐全）。这类缺陷：
//
//   - `go build` / `go vet` / `go test ./internal/...` 全绿（前端不参与）
//   - `node --check app.js` 也通过（**语法完全正确**，错的是执行时机）
//   - curl 后端一切正常
//
// 只有**真实浏览器执行一遍**才能发现。故固化本脚本。
//
// ## 用法
//
// 1. 先启动 workbuddy-api（任意目录，需已导入账号）
// 2. NODE_PATH=<node_modules 路径> node tools/panel_smoke.js
//    （需要 playwright；Windows 上默认用 Edge，无需额外下载 Chromium）
//
// 退出码 0 = 通过；非 0 = 失败并打印原因。
'use strict';

const { chromium } = require('playwright');

const URL = process.env.PANEL_URL || 'http://127.0.0.1:7863/';
const BROWSER = process.env.PANEL_BROWSER ||
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe';

(async () => {
  const errors = [];
  const browser = await chromium.launch({ executablePath: BROWSER, headless: true });
  const page = await browser.newPage();
  page.on('console', (m) => { if (m.type() === 'error') errors.push('console: ' + m.text()); });
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));

  await page.goto(URL, { waitUntil: 'networkidle', timeout: 30000 });
  await page.waitForSelector('.acct-card', { timeout: 15000 }).catch(() => {});

  const info = await page.evaluate(() => ({
    stateLoaded: (typeof state !== 'undefined' && state !== null),
    accounts: (typeof state !== 'undefined' && state) ? (state.accounts || []).length : -1,
    growthSupported: (typeof state !== 'undefined' && state) ? state.growth_supported : 'n/a',
    cards: document.querySelectorAll('.acct-card').length,
    taskBtnDisabled: document.getElementById('btnTasksAll')
      ? document.getElementById('btnTasksAll').disabled : 'missing',
    starBtns: document.querySelectorAll('.acct-card .icon-op[onclick^="runTasks"]').length,
    checkinAllBound: !!document.getElementById('btnCheckinAll').onclick,
    refreshAllBound: !!document.getElementById('btnRefreshAll').onclick,
    settingsBound: !!document.getElementById('btnSettings').onclick,
  }));

  await browser.close();

  console.log('=== 页面状态 ===');
  Object.entries(info).forEach(([k, v]) => console.log('  ' + k.padEnd(18) + v));
  console.log('=== 控制台错误 (' + errors.length + ') ===');
  errors.slice(0, 12).forEach((e) => console.log('  ' + e));

  const bad = [];
  if (!info.stateLoaded) bad.push('state 为 null —— 初始化在中途抛错（检查 bind() 是否读了 state）');
  if (info.cards === 0) bad.push('没有渲染任何账号卡片');
  if (info.accounts !== info.cards) {
    bad.push('账号数(' + info.accounts + ') 与卡片数(' + info.cards + ') 不一致');
  }
  if (info.taskBtnDisabled === true) bad.push('一键做任务按钮被禁用');
  if (!info.checkinAllBound || !info.refreshAllBound || !info.settingsBound) {
    bad.push('部分按钮未绑定（bind() 可能中途抛错）');
  }
  if (errors.length) bad.push('有 ' + errors.length + ' 条控制台错误');

  if (bad.length) {
    console.log('\n❌ 失败：' + bad.join('；'));
    process.exit(1);
  }
  console.log('\n✅ 通过：账号卡片 ' + info.cards + ' 张，一键做任务可用，★ 入口 ' +
    info.starBtns + ' 个，无控制台错误');
})().catch((e) => {
  console.error('❌ 脚本执行失败：', e && e.message ? e.message : e);
  process.exit(2);
});