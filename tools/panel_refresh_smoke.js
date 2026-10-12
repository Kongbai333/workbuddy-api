// 面板前端冒烟测试 #2：验证「任务执行后积分自动刷新」链路的前端部分。
//
// ## 为什么需要它
//
// 2026-10-12 用户实测报障：「执行完任务后，积分没有自动刷新」。
// 根因：面板 `/api/state` 的 credits 读自**后端 pool 缓存**，
// 任务执行本身不改它，只 loadState() 只能看到领奖前的旧值。
//
// 这个缺陷 Go 测试看不见（它测的是后端缓存）、node --check 也看不见（语法没问题），
// 只有真实浏览器里注入增量快照、验证 DOM 真的会重新渲染才能兜住。
//
// ## 只做静态验证，不触发真实任务
//
// 真实执行会改动账号状态（点亮任务、领奖），本脚本只：
//   · 注入一个假的 creditsBefore 快照
//   · 改内存里的 state.accounts 余额
//   · 调 renderAccounts / renderTaskPanel 看 DOM 结果
// 结束后即丢弃，不落任何状态。
//
// ## 用法
//
//   NODE_PATH=<playwright 所在 node_modules> node tools/panel_refresh_smoke.js

const { chromium } = require('playwright');

(async () => {
  const errors = [];
  const browser = await chromium.launch({
    executablePath: 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
    headless: true,
  });
  const page = await browser.newPage();
  page.on('console', (m) => { if (m.type() === 'error') errors.push('console: ' + m.text()); });
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));

  await page.goto('http://127.0.0.1:7863/', { waitUntil: 'networkidle', timeout: 30000 });
  await page.waitForSelector('.acct-card', { timeout: 15000 }).catch(() => {});

  const info = await page.evaluate(async () => {
    const out = {};
    out.finishTaskRunDefined = typeof finishTaskRun === 'function';
    out.creditDeltaDefined = typeof creditDelta === 'function';
    out.snapshotDefined = typeof snapshotCredits === 'function';
    // 手动注入一个增量快照，验证「卡片 +N 角标」真的会渲染
    const uid = state.accounts[0].uid;
    creditsBefore = { [uid]: (state.accounts[0].credits || 0) - 42 };
    state.accounts[0].credits = (state.accounts[0].credits || 0) + 42;
    renderAccounts();
    const gainTag = () => Array.from(document.querySelectorAll('.acct-card .tag')).find(t => (t.getAttribute('title')||'').includes('本轮任务'));
    out.cardGainTag = !!gainTag();
    out.cardGainText = gainTag() ? gainTag().textContent : '';
    // 结果面板也带上 delta 渲染一次
    renderTaskPanel({ running: false, reports: [{ uid, nickname: 'x', msg: 'm', tasks: [] }], credit: 42, energy: 5 }, { [uid]: 42 });
    out.panelGainTag = !!document.querySelector('.task-acct-head .tk-gain');
    clearCreditDelta();
    out.cardGainAfterClear = !!gainTag();
    return out;
  });

  const css = await page.evaluate(() => {
    const el = document.createElement('div');
    el.className = 'task-acct-head';
    const inner = document.createElement('span');
    inner.className = 'tk-gain';
    el.appendChild(inner);
    document.body.appendChild(el);
    const c = getComputedStyle(el.firstChild).color;
    el.remove();
    return c;
  });

  await browser.close();

  console.log('=== 断言 ===');
  Object.entries(info).forEach(([k, v]) => console.log('  ' + k.padEnd(22) + v));
  console.log('  tk-gain 实际颜色       ' + css);
  console.log('=== 控制台错误 (' + errors.length + ') ===');
  errors.slice(0, 8).forEach((e) => console.log('  ' + e));

  const bad = [];
  if (!info.finishTaskRunDefined) bad.push('finishTaskRun 未定义');
  if (!info.creditDeltaDefined || !info.snapshotDefined) bad.push('增量计算函数缺失');
  if (!info.cardGainTag) bad.push('卡片未渲染 +N 角标');
  if (!/^\+\d+$/.test(info.cardGainText.trim())) bad.push('角标未按 +N 格式渲染，实际=' + JSON.stringify(info.cardGainText));
  if (!info.panelGainTag) bad.push('结果面板未渲染增量');
  if (info.cardGainAfterClear) bad.push('清除快照后角标仍残留');
  if (errors.length) bad.push(errors.length + ' 条控制台错误');

  if (bad.length) { console.log('\n❌ 失败：' + bad.join('；')); process.exit(1); }
  console.log('\n✅ 通过：任务结束后的积分刷新链路与增量高亮均正常');
})().catch((e) => { console.error('❌', e && e.message ? e.message : e); process.exit(2); });