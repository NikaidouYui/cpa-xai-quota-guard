# -*- coding: utf-8 -*-
"""Render polished, desensitized screenshots from real console.html + mock data."""
from __future__ import annotations

import json
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[1]
CONSOLE = ROOT / "web" / "console.html"
OUT = ROOT / "docs" / "screenshots"
OUT.mkdir(parents=True, exist_ok=True)

# Fully synthetic / masked sample aligned with paintStatusBar field names
MOCK = {
    "ok": True,
    "version": "0.3.8",
    "enabled": True,
    "now_ms": 1783915200000,
    "view": "focus",
    "plugin_id": "cpa-xai-quota-guard",
    "config": {
        "management_url": "http://cpa.local:8317",
        "management_key_set": True,
        "tick_seconds": 30,
        "max_reset_seconds": 86400,
        "state_path": "/data/cpa-xai-quota-guard/state.json",
        "cpamp_url": "",
        "min_reset_seconds": 60,
        "include_unobserved_quota_est": False,
        "patrol_enabled": True,
        "patrol_interval": 1800,
        "patrol_timeout": 15,
        "patrol_initial_delay_sec": 60,
        "patrol_concurrency": 16,
        "patrol_batch_size": 0,
        "patrol_auth_dir": "/root/.cli-proxy-api",
        "patrol_proxy_url": "socks5://***:***@proxy.example:1080",
        "patrol_proxy_set": True,
        "patrol_model": "grok-4.5",
        "patrol_auto_model_switch": False,
    },
    "summary": {
        "total": 2702,
        "returned": 3,
        "view": "focus",
        "tracked": 1961,
        "inventory_only": 741,
        "auto_disabled": 1951,
        "user_manual": 0,
        "recover_due": 0,
        "rolling_over": 1951,
        "cpa_disabled": 0,
        "tier_free": 2009,
        "tier_super": 0,
        "tier_heavy": 0,
        "tier_unknown": 0,
        "hot_total": 12,
        "hot_shown": 12,
        "hot_hidden": 0,
        "focus_hot_cap": 80,
    },
    "metrics": {
        "xai_total": 2702,
        "xai_enabled": 740,
        "xai_disabled": 1962,
        "quota_total_est": 740_000_000,
        "quota_known_accounts": 1951,
        "unobserved_accounts": 0,
        "default_limit_per_acct": 1_000_000,
        "include_unobserved_est": False,
        "used_today": 72_800_000,
        "used_today_display": 72_800_000,
        "estimated_today": 0,
        "used_total": 898_000_000,
        "used_total_display": 898_000_000,
        "requests_today": 1535,
        "rolling_used_known": 4_150_000_000,
        "rolling_limit_known": 3_900_000_000,
        "quota_used_known": 4_150_000_000,
        "quota_limit_known": 3_900_000_000,
        "rolling_accounts": 1951,
        "day_key": "2026-07-13",
        "backfill_source": "cpamp_backfill",
        "backfill_tokens_floor": 655_000_000,
        "detail_missing_alert": False,
        "zero_token_streak": 0,
    },
    "patrol": {
        "running": False,
        "started_at_ms": 1783914000000,
        "completed_at_ms": 1783914100000,
        "total_candidates": 736,
        "total_probed": 736,
        "total_alive": 720,
        "total_deleted": 0,
        "total_cooldown": 16,
        "total_reenabled": 0,
        "total_errors": 0,
        "total_skipped": 0,
        "total_429_cooldown": 16,
        "total_402_cooldown": 0,
        "workers": 16,
        "workers_max": 16,
        "workers_min": 4,
        "load1": 0.69,
        "scale_reason": "load_idle_max+max",
        "scope": "all",
        "saved_at_ms": 1783914101000,
        "by_http": {"200": 720, "429": 16},
        "by_action": {"alive": 720, "cooldown": 16},
        "recent_log": [
            {
                "time_ms": 1783914098000,
                "account": "user***@example.com",
                "file_name": "xai-user***.json",
                "auth_index": "a1b2c3d4",
                "action": "cooldown",
                "http_code": 429,
                "reason": "free-usage exhausted · rolling 24h",
            },
            {
                "time_ms": 1783914096000,
                "account": "demo***@sample.net",
                "file_name": "xai-demo***.json",
                "auth_index": "e5f6g7h8",
                "action": "alive",
                "http_code": 200,
                "reason": "200 OK · model=grok-4.5",
            },
            {
                "time_ms": 1783914094000,
                "account": "probe***@mail.test",
                "file_name": "xai-probe***.json",
                "auth_index": "i9j0k1l2",
                "action": "alive",
                "http_code": 200,
                "reason": "200 OK · model=grok-4.5",
            },
            {
                "time_ms": 1783914092000,
                "account": "acct***@demo.org",
                "file_name": "xai-acct***.json",
                "auth_index": "m3n4o5p6",
                "action": "cooldown",
                "http_code": 429,
                "reason": "subscription:free-usage-exhausted",
            },
        ],
    },
    "accounts": [
        {
            "auth_index": "a1b2c3d4",
            "account": "user***@example.com",
            "file_name": "xai-user***.example.com.json",
            "provider": "xai",
            "state": "auto_disabled",
            "disable_source": "plugin_auto",
            "health": "auto",
            "signal": "body.error.code=subscription:free-usage-exhausted",
            "reason": "免费额度用尽(rolling 24h)，约 24h 后可恢复 · subscription:free-usage-exhausted",
            "recover_at_ms": 1784001600000,
            "used_today": 1_080_000,
            "requests_today": 7,
            "rolling_actual": 2_150_000,
            "rolling_limit": 2_000_000,
            "rolling_over": True,
            "cpa_success": 1,
            "tier": "free",
            "tier_protected": False,
            "disabled": True,
        },
        {
            "auth_index": "e5f6g7h8",
            "account": "demo***@sample.net",
            "file_name": "xai-demo***.sample.net.json",
            "provider": "xai",
            "state": "auto_disabled",
            "disable_source": "plugin_auto",
            "health": "auto",
            "signal": "body.error.code=subscription:free-usage-exhausted",
            "reason": "免费额度用尽(rolling 24h)，约 24h 后可恢复 · subscription:free-usage-exhausted",
            "recover_at_ms": 1784002500000,
            "used_today": 1_050_000,
            "requests_today": 8,
            "rolling_actual": 2_040_000,
            "rolling_limit": 2_000_000,
            "rolling_over": True,
            "cpa_success": 0,
            "tier": "free",
            "tier_protected": False,
            "disabled": True,
        },
        {
            "auth_index": "i9j0k1l2",
            "account": "probe***@mail.test",
            "file_name": "xai-probe***.mail.test.json",
            "provider": "xai",
            "state": "auto_disabled",
            "disable_source": "plugin_auto",
            "health": "auto",
            "signal": "body.error.code=subscription:free-usage-exhausted",
            "reason": "免费额度用尽(rolling 24h)，约 24h 后可恢复 · subscription:free-usage-exhausted",
            "recover_at_ms": 1784003400000,
            "used_today": 1_030_000,
            "requests_today": 10,
            "rolling_actual": 2_100_000,
            "rolling_limit": 2_000_000,
            "rolling_over": True,
            "cpa_success": 2,
            "tier": "free",
            "tier_protected": False,
            "disabled": True,
        },
    ],
    "delete_history": [],
    "action_history": [
        {
            "time_ms": 1783914098000,
            "account": "user***@example.com",
            "source": "patrol",
            "action": "cooldown",
            "reason": "429 free-usage",
            "http_code": 429,
            "file_name": "xai-user***.json",
            "auth_index": "a1b2c3d4",
        },
        {
            "time_ms": 1783913000000,
            "account": "demo***@sample.net",
            "source": "usage",
            "action": "cooldown",
            "reason": "429 free-usage",
            "http_code": 429,
            "file_name": "xai-demo***.json",
            "auth_index": "e5f6g7h8",
        },
    ],
}


def build_demo_html() -> Path:
    html = CONSOLE.read_text(encoding="utf-8")
    mock_js = f"""
<script>
(function(){{
  try {{
    document.documentElement.setAttribute('data-theme','light');
    document.documentElement.style.colorScheme = 'light';
  }} catch(e) {{}}
  window.detectHostTheme = function(){{ return 'light'; }};
  window.syncHostTheme = function(){{
    document.documentElement.setAttribute('data-theme','light');
  }};
  window.ensurePatrolFollow = function(){{}};
  var MOCK = {json.dumps(MOCK, ensure_ascii=False)};
  window.api = async function(path, opts){{
    path = String(path||'');
    if(path.indexOf('state') === 0){{ return {{ok:true, result: MOCK}}; }}
    if(path.indexOf('patrol/status') === 0){{ return {{ok:true, patrol: MOCK.patrol, result: {{patrol: MOCK.patrol}}}}; }}
    if(path.indexOf('patrol/config') === 0 || path === 'config' || path === 'settings'){{
      return {{ok:true, result: MOCK.config}};
    }}
    if(path === 'health'){{ return {{ok:true, result: {{ok:true, enabled:true, version: MOCK.version}}}}; }}
    return {{ok:true, result: {{}}}};
  }};
  function bootMock(){{
    try {{ localStorage.setItem('cpaXaiQgKey', 'docs-demo-key'); }} catch(e) {{}}
    try {{
      if(typeof normalizeStatePayload !== 'function') return;
      var d = normalizeStatePayload(MOCK);
      LAST_STATE = d;
      LAST_FETCH_AT = Date.now();
      applyEnabledUI(true);
      var vb = document.getElementById('verBadge');
      if(vb) vb.textContent = 'v' + (d.version||'0.3.8');
      paintStatusBar(d);
      renderActionLog(d.delete_history, d.action_history);
      if(d.patrol) paintPatrol(d.patrol, d);
      var cfg = d.config || {{}};
      var en = document.getElementById('cfgPatrolEn');
      if(en) en.checked = !!cfg.patrol_enabled;
      var el;
      el=document.getElementById('cfgPatrolInt'); if(el) el.value = Math.max(1, Math.round((Number(cfg.patrol_interval)||3600)/60));
      el=document.getElementById('cfgPatrolTO'); if(el) el.value = cfg.patrol_timeout || 15;
      el=document.getElementById('cfgPatrolInitDelay'); if(el) el.value = cfg.patrol_initial_delay_sec!=null?cfg.patrol_initial_delay_sec:60;
      el=document.getElementById('cfgPatrolDir'); if(el) el.value = cfg.patrol_auth_dir || '';
      el=document.getElementById('cfgPatrolCon'); if(el) el.value = cfg.patrol_concurrency || 16;
      el=document.getElementById('cfgPatrolBatch'); if(el) el.value = cfg.patrol_batch_size!=null?cfg.patrol_batch_size:0;
      el=document.getElementById('cfgPatrolProxy'); if(el) el.value = cfg.patrol_proxy_url || '';
      el=document.getElementById('cfgPatrolModel'); if(el){{ try{{ ensurePatrolModelOption(cfg.patrol_model||'grok-4.5'); }}catch(e){{}} el.value = cfg.patrol_model||'grok-4.5'; }}
      el=document.getElementById('cfgPatrolAutoModel'); if(el) el.checked = !!cfg.patrol_auto_model_switch;
      PATROL_CFG_APPLIED = true;
      PATROL_FORM_DIRTY = false;
      var ph = document.getElementById('patrolCfgHint');
      if(ph) ph.textContent = '定时巡查开 · 周期30分钟 · 并发上限(弹性)16 · 模型grok-4.5 · 自动换模关 · 代理已填(脱敏示例)';
      var meta = document.getElementById('cfgMeta');
      if(meta) meta.textContent = 'management_url=http://cpa.local:8317 · key_set=true · demo screenshot (synthetic data)';
      renderAccountTable();
      document.body.setAttribute('data-mock-ready','1');
    }} catch(e) {{
      console.error(e);
      document.body.setAttribute('data-mock-error', String(e && e.message || e));
    }}
  }}
  if(document.readyState === 'complete') setTimeout(bootMock, 30);
  else window.addEventListener('load', function(){{ setTimeout(bootMock, 30); }});
  var n=0; var t=setInterval(function(){{
    n++; bootMock();
    if(document.body.getAttribute('data-mock-ready')==='1' || n>30) clearInterval(t);
  }}, 80);
}})();
</script>
"""
    if "</body>" not in html:
        raise SystemExit("console.html missing </body>")
    html = html.replace("</body>", mock_js + "\n</body>", 1)
    demo = OUT / "_render_demo.html"
    demo.write_text(html, encoding="utf-8")
    return demo


def main() -> None:
    demo = build_demo_html()
    print("demo", demo, demo.stat().st_size)
    chrome = r"C:\Program Files\Google\Chrome\Application\chrome.exe"
    with sync_playwright() as p:
        browser = p.chromium.launch(
            headless=True,
            executable_path=chrome,
            args=["--disable-gpu", "--hide-scrollbars"],
        )
        page = browser.new_page(viewport={"width": 1280, "height": 1700}, device_scale_factor=2)
        page.goto(demo.as_uri(), wait_until="domcontentloaded", timeout=30000)
        page.wait_for_selector("body[data-mock-ready='1']", timeout=20000)
        page.wait_for_timeout(500)

        box_top = page.locator(".topbar").bounding_box()
        box_card1 = page.locator(".grid > .card").nth(0).bounding_box()
        if not box_top or not box_card1:
            raise SystemExit("dashboard boxes missing")
        x = min(box_top["x"], box_card1["x"]) - 10
        y = max(0, box_top["y"] - 10)
        w = max(box_top["width"], box_card1["width"]) + 20
        h = (box_card1["y"] + box_card1["height"]) - y + 10
        page.screenshot(
            path=str(OUT / "dashboard.png"),
            clip={"x": max(0, x), "y": y, "width": w, "height": h},
            type="png",
        )
        print("dashboard", (OUT / "dashboard.png").stat().st_size)

        page.locator("#patrolCard").scroll_into_view_if_needed()
        page.wait_for_timeout(250)
        page.locator("#patrolCard").screenshot(path=str(OUT / "patrol.png"), type="png")
        print("patrol", (OUT / "patrol.png").stat().st_size)

        acc = page.locator("#accBody").locator("xpath=ancestor::div[contains(@class,'card')][1]")
        acc.scroll_into_view_if_needed()
        page.wait_for_timeout(250)
        acc.screenshot(path=str(OUT / "accounts.png"), type="png")
        print("accounts", (OUT / "accounts.png").stat().st_size)

        browser.close()
    print("ok")


if __name__ == "__main__":
    main()
