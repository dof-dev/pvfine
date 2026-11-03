/**
 * pvfine Official Website (v2.0) — Interactive Engine & Smart GitHub Release Loader
 * Zero external dependencies, pure modern Web APIs (Canvas 2D, View Transitions, Dialog, Clipboard)
 */

document.addEventListener('DOMContentLoaded', () => {
  const detectedOs = initOsDetection();
  initCyberBackgroundCanvas();
  initCursorSpotlightAndTilt();
  initScrollProgressAndNav();
  initThemeToggle();
  initVariantSpectrum();
  initAppSimulator();
  initTokenDecompilerLab();
  initBenchmarkRunner();
  initSdkTabsAndCopy();
  initCommandPalette();
  initReleaseNotesModal();
  loadGitHubReleaseInfo(detectedOs);
});

/* ==========================================================================
   1. OS Detection & Shortcut Adaptation
   ========================================================================== */
function initOsDetection() {
  const ua = (navigator.userAgent || '').toLowerCase();
  const platform = (navigator.userAgentData?.platform || navigator.platform || '').toLowerCase();
  let detectedOs = 'windows';

  if (ua.includes('mac') || ua.includes('darwin') || platform.includes('mac')) {
    detectedOs = 'macos';
  } else if (ua.includes('linux') || platform.includes('linux')) {
    detectedOs = 'linux';
  }

  const shortcutHint = document.getElementById('cmd-shortcut-hint');
  if (shortcutHint) {
    shortcutHint.textContent = detectedOs === 'macos' ? '⌘K' : 'Ctrl+K';
  }

  const downloadCards = document.querySelectorAll('.download-card');
  downloadCards.forEach(card => {
    if (card.dataset.os === detectedOs) {
      card.classList.add('highlight');
      const badge = card.querySelector('.recommended-badge');
      if (badge) badge.style.display = 'inline-block';
    }
  });

  return detectedOs;
}

/* ==========================================================================
   2. GitHub Releases API — Auto-Parse Latest Version & Direct Asset URLs
   ========================================================================== */
async function loadGitHubReleaseInfo(detectedOs) {
  const repo = 'dof-dev/pvfine';
  const apiUrl = `https://api.github.com/repos/${repo}/releases/latest`;
  const defaultReleaseUrl = `https://github.com/${repo}/releases/latest`;
  const cacheKey = 'pvfine_latest_release_v3';

  // Try session cache first to avoid GitHub API 60/hr rate limit on rapid refreshes
  try {
    const cached = sessionStorage.getItem(cacheKey);
    if (cached) {
      const parsed = JSON.parse(cached);
      if (parsed && parsed.timestamp && Date.now() - parsed.timestamp < 10 * 60 * 1000) {
        applyReleaseData(parsed.data, detectedOs, defaultReleaseUrl);
        return;
      }
    }
  } catch (_) {
    // Ignore storage errors
  }

  try {
    const response = await fetch(apiUrl, {
      headers: { 'Accept': 'application/vnd.github+json' }
    });
    if (!response.ok) {
      console.warn(`GitHub API returned ${response.status}; falling back to default release links.`);
      return;
    }
    const data = await response.json();
    try {
      sessionStorage.setItem(cacheKey, JSON.stringify({ timestamp: Date.now(), data }));
    } catch (_) {}
    applyReleaseData(data, detectedOs, defaultReleaseUrl);
  } catch (error) {
    console.warn('Failed to fetch GitHub release info:', error);
  }
}

function formatFileSize(bytes) {
  if (!bytes || bytes <= 0) return '';
  const mb = bytes / (1024 * 1024);
  return `${mb.toFixed(1)} MB`;
}

function applyReleaseData(data, detectedOs, defaultReleaseUrl) {
  const tag = data.tag_name || 'v1.0.0';
  const releaseUrl = data.html_url || defaultReleaseUrl;
  const assets = Array.isArray(data.assets) ? data.assets : [];

  // 1. Update Version Badges across the page
  const headerVersionPill = document.getElementById('release-version-pill');
  if (headerVersionPill) headerVersionPill.textContent = tag;

  const heroBadgeVersion = document.getElementById('hero-badge-version');
  if (heroBadgeVersion) heroBadgeVersion.textContent = `NEW ${tag}`;

  const dlTelemetryTag = document.getElementById('dl-telemetry-tag');
  if (dlTelemetryTag) dlTelemetryTag.textContent = tag;

  const dlTelemetryDate = document.getElementById('dl-telemetry-date');
  if (dlTelemetryDate && data.published_at) {
    const dt = new Date(data.published_at);
    if (!isNaN(dt.getTime())) {
      const yyyy = dt.getFullYear();
      const mm = String(dt.getMonth() + 1).padStart(2, '0');
      const dd = String(dt.getDate()).padStart(2, '0');
      dlTelemetryDate.textContent = `发布于 ${yyyy}-${mm}-${dd} · 共 ${assets.length} 个构建产物`;
    }
  }

  // Populate Release Notes Modal
  const notesTitle = document.getElementById('release-notes-title');
  const notesBody = document.getElementById('release-notes-body');
  if (notesTitle) notesTitle.textContent = `pvfine ${tag} 版本发布说明`;
  if (notesBody && data.body) {
    notesBody.textContent = data.body.trim();
  }

  // 2. Match assets from .github/workflows/release.yml
  const macUniversal = assets.find(a =>
    a.name.endsWith('.dmg') && (a.name.includes('universal') || a.name.includes('macos') || a.name.includes('darwin'))
  ) || assets.find(a => a.name.endsWith('.dmg'));

  const macPortableZip = assets.find(a =>
    a.name.endsWith('.zip') && (a.name.includes('darwin') || a.name.includes('macos'))
  ) || assets.find(a => a.name.endsWith('.dmg') && a.name.includes('amd64')) || macUniversal;

  const winInstaller = assets.find(a =>
    a.name.endsWith('.exe') && (a.name.includes('installer') || a.name.includes('setup'))
  );
  const winPortable = assets.find(a =>
    (a.name.endsWith('.exe') || a.name.endsWith('.zip')) &&
    a.name.includes('windows') &&
    !a.name.includes('installer') &&
    !a.name.includes('setup')
  );

  const linuxAppImage = assets.find(a => a.name.toLowerCase().endsWith('.appimage'));
  const linuxDeb = assets.find(a => a.name.endsWith('.deb') || a.name.endsWith('.tar.gz'));
  const sha256Asset = assets.find(a => a.name.toUpperCase().includes('SHA256SUMS'));

  // 3. Bind Download Matrix Buttons
  bindAssetButton('[data-asset-type="mac-universal"]', macUniversal, 'macOS 通用安装包 (.dmg)', releaseUrl);
  bindAssetButton(
    '[data-asset-type="mac-intel"]',
    macPortableZip,
    macPortableZip && macPortableZip.name.endsWith('.zip') ? 'macOS 应用归档包 (.app.zip)' : 'macOS 备用安装包 (.dmg)',
    releaseUrl
  );
  bindAssetButton('[data-asset-type="win-installer"]', winInstaller, 'Windows 自动安装包 (.exe)', releaseUrl);
  bindAssetButton('[data-asset-type="win-portable"]', winPortable, '便携独立运行版 (.exe)', releaseUrl);
  bindAssetButton('[data-asset-type="linux-appimage"]', linuxAppImage, 'Universal AppImage', releaseUrl);
  bindAssetButton('[data-asset-type="linux-deb"]', linuxDeb, '查看 Linux 发行版与校验码', releaseUrl);

  const sha256Link = document.getElementById('release-sha256-link');
  if (sha256Link && sha256Asset && sha256Asset.browser_download_url) {
    sha256Link.href = sha256Asset.browser_download_url;
    sha256Link.title = `直接下载 ${sha256Asset.name}`;
  }

  // 4. Bind Hero CTA & Header CTA to Direct OS Asset Link
  const heroDownloadBtn = document.getElementById('hero-download-btn');
  const heroDownloadText = document.getElementById('hero-download-text');
  const navDownloadBtn = document.getElementById('nav-download-btn');
  const heroDownloadDesc = document.getElementById('hero-download-hint');

  let targetAsset = null;
  let osLabel = '最新版本';

  if (detectedOs === 'macos') {
    targetAsset = macUniversal || macPortableZip;
    osLabel = 'macOS 通用版';
  } else if (detectedOs === 'windows') {
    targetAsset = winInstaller || winPortable;
    osLabel = 'Windows 64-bit';
  } else if (detectedOs === 'linux') {
    targetAsset = linuxAppImage || linuxDeb;
    osLabel = 'Linux AppImage';
  }

  if (heroDownloadBtn) {
    if (targetAsset && targetAsset.browser_download_url) {
      heroDownloadBtn.href = targetAsset.browser_download_url;
      heroDownloadBtn.setAttribute('download', '');
      const sizeStr = formatFileSize(targetAsset.size);
      const labelText = `下载 ${osLabel} (${tag}${sizeStr ? ' · ' + sizeStr : ''})`;
      if (heroDownloadText) {
        heroDownloadText.textContent = labelText;
      } else {
        heroDownloadBtn.textContent = labelText;
      }
      if (navDownloadBtn) {
        navDownloadBtn.href = targetAsset.browser_download_url;
        navDownloadBtn.textContent = `下载 ${tag}`;
      }
    } else {
      heroDownloadBtn.href = releaseUrl;
      const labelText = `下载 ${osLabel} (${tag})`;
      if (heroDownloadText) {
        heroDownloadText.textContent = labelText;
      } else {
        heroDownloadBtn.textContent = labelText;
      }
      if (navDownloadBtn) {
        navDownloadBtn.href = releaseUrl;
      }
    }
  }

  if (heroDownloadDesc) {
    heroDownloadDesc.textContent = `当前最新版本 ${tag} · 已自动直链 GitHub Release 预编译安装包 · GPL-3.0 开源`;
  }
}

function bindAssetButton(selector, asset, defaultLabel, fallbackUrl) {
  const btn = document.querySelector(selector);
  if (!btn) return;

  if (asset && asset.browser_download_url) {
    btn.href = asset.browser_download_url;
    const size = formatFileSize(asset.size);
    btn.textContent = size ? `${defaultLabel} (${size})` : defaultLabel;
    btn.setAttribute('download', '');
    btn.title = `直接下载: ${asset.name}`;
  } else {
    btn.href = fallbackUrl;
    btn.title = '前往 GitHub Release 页面查看';
  }
}

/* ==========================================================================
   3. Interactive Cyber Background Canvas (Particles + 5-Byte Token Streams)
   ========================================================================== */
function initCyberBackgroundCanvas() {
  const canvas = document.getElementById('cyber-bg-canvas');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');
  if (!ctx) return;

  let width = (canvas.width = window.innerWidth);
  let height = (canvas.height = window.innerHeight);

  const tokens = [
    '0x53 0x34 0x41 0x32 0x31',
    '#PVF_File',
    'sTrW::UTF-16LE',
    'Paged110::AES256',
    '115CN-mod',
    '[grade] 5',
    '{8=<31::id>}',
    'MCP :19245',
    'ChunkLRU 99.6%'
  ];

  const nodeCount = Math.min(42, Math.floor((width * height) / 34000));
  const nodes = Array.from({ length: nodeCount }, (_, i) => ({
    x: Math.random() * width,
    y: Math.random() * height,
    vx: (Math.random() - 0.5) * 0.35,
    vy: (Math.random() - 0.5) * 0.35,
    r: Math.random() * 1.6 + 0.8,
    label: i % 5 === 0 ? tokens[i % tokens.length] : null
  }));

  let mouseX = width * 0.5;
  let mouseY = height * 0.3;

  window.addEventListener('mousemove', e => {
    mouseX = e.clientX;
    mouseY = e.clientY;
  }, { passive: true });

  window.addEventListener('resize', () => {
    width = canvas.width = window.innerWidth;
    height = canvas.height = window.innerHeight;
  }, { passive: true });

  function render() {
    if (!document.hidden) {
      ctx.clearRect(0, 0, width, height);
      const isLight = document.documentElement.getAttribute('data-theme') === 'light';
      const dotColor = isLight ? 'rgba(37, 99, 235, 0.35)' : 'rgba(96, 165, 250, 0.45)';
      const textColor = isLight ? 'rgba(37, 99, 235, 0.28)' : 'rgba(56, 189, 248, 0.25)';

      ctx.font = '10px "JetBrains Mono", monospace';

      for (let i = 0; i < nodes.length; i++) {
        const n = nodes[i];
        n.x += n.vx;
        n.y += n.vy;

        if (n.x < 0 || n.x > width) n.vx *= -1;
        if (n.y < 0 || n.y > height) n.vy *= -1;

        ctx.beginPath();
        ctx.arc(n.x, n.y, n.r, 0, Math.PI * 2);
        ctx.fillStyle = dotColor;
        ctx.fill();

        if (n.label) {
          ctx.fillStyle = textColor;
          ctx.fillText(n.label, n.x + 6, n.y + 3);
        }

        // Connect nearby nodes
        for (let j = i + 1; j < nodes.length; j++) {
          const m = nodes[j];
          const dx = n.x - m.x;
          const dy = n.y - m.y;
          const distSq = dx * dx + dy * dy;
          if (distSq < 18000) {
            const alpha = (1 - distSq / 18000) * (isLight ? 0.1 : 0.14);
            ctx.strokeStyle = isLight ? `rgba(37, 99, 235, ${alpha})` : `rgba(59, 130, 246, ${alpha})`;
            ctx.lineWidth = 0.7;
            ctx.beginPath();
            ctx.moveTo(n.x, n.y);
            ctx.lineTo(m.x, m.y);
            ctx.stroke();
          }
        }

        // Connect to mouse cursor
        const mdx = n.x - mouseX;
        const mdy = n.y - mouseY;
        const mDistSq = mdx * mdx + mdy * mdy;
        if (mDistSq < 28000) {
          const alpha = (1 - mDistSq / 28000) * 0.24;
          ctx.strokeStyle = `rgba(34, 211, 238, ${alpha})`;
          ctx.lineWidth = 0.9;
          ctx.beginPath();
          ctx.moveTo(n.x, n.y);
          ctx.lineTo(mouseX, mouseY);
          ctx.stroke();
        }
      }
    }
    requestAnimationFrame(render);
  }

  requestAnimationFrame(render);
}

/* ==========================================================================
   4. Cursor Spotlight, Card Border Glow & 3D IDE Tilt
   ========================================================================== */
function initCursorSpotlightAndTilt() {
  const spotlight = document.getElementById('cursor-spotlight');
  const spotlightCards = document.querySelectorAll('.spotlight-card');
  const ideWindow = document.getElementById('ide-window');
  const tiltBtn = document.getElementById('toggle-3d-tilt-btn');

  let tiltEnabled = true;
  if (tiltBtn) {
    tiltBtn.addEventListener('click', () => {
      tiltEnabled = !tiltEnabled;
      tiltBtn.textContent = tiltEnabled ? '3D 视差: ON' : '3D 视差: OFF';
      if (!tiltEnabled && ideWindow) {
        ideWindow.style.transform = 'rotateX(0deg) rotateY(0deg)';
      }
    });
  }

  window.addEventListener('mousemove', e => {
    if (spotlight) {
      spotlight.style.left = `${e.clientX}px`;
      spotlight.style.top = `${e.clientY}px`;
    }

    spotlightCards.forEach(card => {
      const rect = card.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;
      card.style.setProperty('--mouse-x', `${x}px`);
      card.style.setProperty('--mouse-y', `${y}px`);
    });
  }, { passive: true });

  if (ideWindow) {
    const wrapper = ideWindow.parentElement;
    wrapper.addEventListener('mousemove', e => {
      if (!tiltEnabled || window.innerWidth < 1024) return;
      const rect = wrapper.getBoundingClientRect();
      const normX = (e.clientX - rect.left) / rect.width - 0.5;
      const normY = (e.clientY - rect.top) / rect.height - 0.5;
      const rotY = (normX * 4.5).toFixed(2);
      const rotX = (-normY * 4.5).toFixed(2);
      ideWindow.style.transform = `rotateX(${rotX}deg) rotateY(${rotY}deg)`;
    }, { passive: true });

    wrapper.addEventListener('mouseleave', () => {
      ideWindow.style.transform = 'rotateX(0deg) rotateY(0deg)';
    });
  }
}

/* ==========================================================================
   5. Scroll Progress Bar & Sticky Header
   ========================================================================== */
function initScrollProgressAndNav() {
  const header = document.getElementById('site-header');
  const progressBar = document.getElementById('scroll-progress');

  window.addEventListener('scroll', () => {
    const scrollTop = window.scrollY;
    if (header) {
      header.classList.toggle('scrolled', scrollTop > 20);
    }
    if (progressBar) {
      const docHeight = document.documentElement.scrollHeight - window.innerHeight;
      const pct = docHeight > 0 ? Math.min(100, (scrollTop / docHeight) * 100) : 0;
      progressBar.style.width = `${pct}%`;
    }
  }, { passive: true });
}

/* ==========================================================================
   6. Theme Toggle with View Transitions API
   ========================================================================== */
function initThemeToggle() {
  const toggleBtn = document.getElementById('theme-toggle-btn');
  if (!toggleBtn) return;

  const savedTheme = localStorage.getItem('pvfine-theme');
  const systemPrefersDark = !window.matchMedia || window.matchMedia('(prefers-color-scheme: dark)').matches;
  const initialTheme = savedTheme || (systemPrefersDark ? 'dark' : 'light');

  applyThemeToPage(initialTheme);

  toggleBtn.addEventListener('click', () => {
    const nextTheme = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
    if (document.startViewTransition) {
      document.startViewTransition(() => {
        applyThemeToPage(nextTheme);
      });
    } else {
      applyThemeToPage(nextTheme);
    }
    localStorage.setItem('pvfine-theme', nextTheme);
  });
}

function applyThemeToPage(theme) {
  document.documentElement.setAttribute('data-theme', theme);
  document.documentElement.style.colorScheme = theme;
  const svg = document.getElementById('theme-icon-svg');
  if (svg) {
    if (theme === 'dark') {
      svg.innerHTML = '<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>';
    } else {
      svg.innerHTML = '<circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>';
    }
  }
}

/* ==========================================================================
   7. Interactive PVF Format Spectrum Bar
   ========================================================================== */
function initVariantSpectrum() {
  const pills = document.querySelectorAll('.variant-pill');
  const hud = document.getElementById('variant-hud-display');
  if (!pills.length || !hud) return;

  const specs = {
    '60act': {
      title: '60ACT 经典归档',
      desc: '兼容早期 60ACT 容器头部与脚本语法，支持直接读取、结构化编辑与无损写回',
      code: 'Header: 60ACT · Legacy Token Stream'
    },
    '86': {
      title: '86 经典版本归档',
      desc: '标准 S4A21 CRC32 + ROR32 XOR 密码流，完整适配 86 版 19 列强化/增幅表与深渊掉率',
      code: 'Key: 0x81A79011 · sTrW Dual-Pool'
    },
    '90us': {
      title: '90US 标准归档',
      desc: '经典美服/韩服结构，支持 CP949 / UTF-8 / GBK 显示编码自由切换与双池防乱码',
      code: 'S4A21 Standard · 100% Byte-Exact'
    },
    '90cn': {
      title: '90CN 魔改变体归档',
      desc: '基于 nkpi 明文魔数与双字节前缀特征，无需提供外部未知私钥即可自动反推段种子',
      code: 'Auto Seed Recovery · Zero Config'
    },
    '100cn': {
      title: '100CN 分页归档',
      desc: '内置 100CN 解封配套资源，保留双字符串池及 ASCII indexhash 引用，支持离线编辑写回',
      code: 'Paged100CN · Dual Pool + indexhash'
    },
    '110us': {
      title: '110US (Paged110) 归档',
      desc: '10MiB 分页保护容器，内置 sk.dat 私钥与 52 组 AES-256-CBC 密钥链解密，支持行内译文就地修改',
      code: 'AES-256-CBC · 110USextend MOD'
    },
    '115us': {
      title: '115US 分页归档',
      desc: '内置 115US 配套解封资源与容量边界检查，完整支持分页重打包与安全落盘',
      code: 'Paged115US · Built-in Keys'
    },
    '115cn': {
      title: '115CN 分页归档',
      desc: '内置配套解封密钥与双池映射，适配 115CN 复杂商店/锻造/任务 GUI，支持 115CN-mod 导入导出',
      code: '115CN-mod ZIP · 自动合并递增版本'
    },
    'tw': {
      title: 'TW 变体归档',
      desc: '原生支持 TW 专属 Type-3 Token 解析、Big5/UTF-16LE 编码转换、归档差异对比与无损保存',
      code: 'TW Type-3 Token · Lossless RW'
    }
  };

  pills.forEach(pill => {
    pill.addEventListener('click', () => {
      pills.forEach(p => p.classList.remove('active'));
      pill.classList.add('active');
      const info = specs[pill.dataset.variant];
      if (info) {
        hud.innerHTML = `
          <span>当前检视规格: <strong style="color: var(--primary-bright);">${info.title}</strong></span>
          <span>&bull;</span>
          <span>${info.desc}</span>
          <span>&bull;</span>
          <code>${info.code}</code>
        `;
      }
    });
  });
}

/* ==========================================================================
   8. Hero Interactive IDE Simulator (6 Live Workspaces)
   ========================================================================== */
let switchIdeViewGlobal = null;

function initAppSimulator() {
  const treeItems = document.querySelectorAll('.tree-item[data-view]');
  const appTabs = document.querySelectorAll('.app-tab[data-view]');
  const mockupViews = document.querySelectorAll('.mockup-view');
  const windowFileName = document.getElementById('window-active-filename');
  const treeFilterInput = document.getElementById('tree-filter-input');

  const fileTitles = {
    'equ': 'equipment/character/common/amulet/100300001.equ',
    'ani': 'character/swordman/effect/animation/skill_slash.ani',
    'gui': 'skill/swordman/swordman_tree.skl [Visual GUI Suite]',
    'diff': 'CompareWorkbench: Script_115CN_Base.pvf ↔ SeasonMod.pvf',
    'mcp': 'http://127.0.0.1:19245/mcp [AI Streamable HTTP Server]',
    'script': 'workspace/scripts/batch_boost.pvf.js [Goja Sandbox]'
  };

  function switchView(viewName) {
    const updateDom = () => {
      treeItems.forEach(item => item.classList.toggle('active', item.dataset.view === viewName));
      appTabs.forEach(tab => tab.classList.toggle('active', tab.dataset.view === viewName));
      mockupViews.forEach(view => view.classList.toggle('active', view.classList.contains(`view-${viewName}`)));
      if (windowFileName && fileTitles[viewName]) {
        windowFileName.textContent = fileTitles[viewName];
      }
    };

    if (document.startViewTransition) {
      document.startViewTransition(updateDom);
    } else {
      updateDom();
    }
  }

  switchIdeViewGlobal = switchView;

  treeItems.forEach(item => {
    item.addEventListener('click', () => switchView(item.dataset.view));
  });
  appTabs.forEach(tab => {
    tab.addEventListener('click', () => switchView(tab.dataset.view));
  });

  if (treeFilterInput) {
    treeFilterInput.addEventListener('input', () => {
      const q = treeFilterInput.value.trim().toLowerCase();
      treeItems.forEach(item => {
        const text = item.textContent.toLowerCase();
        item.style.display = !q || text.includes(q) ? 'flex' : 'none';
      });
    });
  }

  initEquInteractiveWorkspace();
  initAniCanvasEngine();
  initGuiSuiteWorkspace();
  initCompareWorkbenchDemo();
  initMcpPlaygroundDemo();
  initGojaSandboxDemo();
}

/* --- Workspace 1: EQU Live Editor + Particle Aura --- */
function initEquInteractiveWorkspace() {
  const rarityBtn = document.getElementById('equ-btn-rarity');
  const atkBtn = document.getElementById('equ-btn-atk');
  const elemBtn = document.getElementById('equ-btn-elem');
  const nameBtn = document.getElementById('equ-btn-name');

  const rarities = [
    { grade: 5, label: '史诗 (Epic)', badge: 'EPIC', color: '#fbbf24', className: '', hex: '02 05 00 00 00' },
    { grade: 6, label: '神话 (Mythic)', badge: 'MYTHIC', color: '#f472b6', className: 'rarity-mythic', hex: '02 06 00 00 00' },
    { grade: 7, label: '太初 (Primeval)', badge: 'PRIMEVAL', color: '#22d3ee', className: 'rarity-primeval', hex: '02 07 00 00 00' }
  ];

  const names = [
    { cn: '王者之星·神圣护符', ref: '{8=<31::amulet_star_01>}' },
    { cn: '次元星冕·永恒神谕', ref: '{8=<31::amulet_oracle_ex>}' },
    { cn: '深渊暴君·破界之核', ref: '{8=<31::amulet_abyss_core>}' }
  ];

  let rarityIdx = 0;
  let nameIdx = 0;
  let patk = 380;
  let matk = 410;
  let satk = 265;
  let elem = 35;

  function toLeHex32(num) {
    const b0 = (num & 0xff).toString(16).padStart(2, '0').toUpperCase();
    const b1 = ((num >> 8) & 0xff).toString(16).padStart(2, '0').toUpperCase();
    const b2 = ((num >> 16) & 0xff).toString(16).padStart(2, '0').toUpperCase();
    const b3 = ((num >> 24) & 0xff).toString(16).padStart(2, '0').toUpperCase();
    return `02 ${b0} ${b1} ${b2} ${b3}`;
  }

  let currentAuraColor = '#fbbf24';

  if (rarityBtn) {
    rarityBtn.addEventListener('click', () => {
      rarityIdx = (rarityIdx + 1) % rarities.length;
      const r = rarities[rarityIdx];
      currentAuraColor = r.color;
      rarityBtn.textContent = `切换品级: ${r.label.split(' ')[0]} (${r.grade})`;

      const codeGrade = document.getElementById('code-equ-grade');
      const codeGradeTag = document.getElementById('code-equ-grade-tag');
      const previewBadge = document.getElementById('preview-rarity-badge');
      const previewName = document.getElementById('preview-equ-name');
      const previewSub = document.getElementById('preview-equ-subtitle');
      const card = document.getElementById('live-equip-card');
      const hexGrade = document.getElementById('hex-grade-bytes');

      if (codeGrade) codeGrade.textContent = r.grade;
      if (codeGradeTag) codeGradeTag.textContent = r.label;
      if (previewBadge) {
        previewBadge.textContent = r.badge;
        previewBadge.style.color = r.color;
      }
      if (previewName) previewName.style.color = r.color;
      if (previewSub) previewSub.textContent = `项链 / ${r.label} / Lv.110`;
      if (card) {
        card.className = `equip-card ${r.className}`;
      }
      if (hexGrade) hexGrade.textContent = r.hex;
    });
  }

  if (atkBtn) {
    atkBtn.addEventListener('click', () => {
      patk = Math.round(patk * 1.25);
      matk = Math.round(matk * 1.25);
      satk = Math.round(satk * 1.25);
      if (patk > 9999) { patk = 380; matk = 410; satk = 265; }

      document.getElementById('code-equ-patk').textContent = patk;
      document.getElementById('code-equ-matk').textContent = matk;
      document.getElementById('code-equ-satk').textContent = satk;
      document.getElementById('preview-equ-patk').textContent = `+${patk}`;
      document.getElementById('preview-equ-matk').textContent = `+${matk}`;
      document.getElementById('preview-equ-satk').textContent = `+${satk}`;
      document.getElementById('hex-patk-bytes').textContent = toLeHex32(patk);
    });
  }

  if (elemBtn) {
    elemBtn.addEventListener('click', () => {
      elem = elem >= 125 ? 35 : elem + 15;
      document.getElementById('code-equ-elem').textContent = elem;
      document.getElementById('preview-equ-elem').textContent = `+${elem}`;
      document.getElementById('hex-elem-bytes').textContent = toLeHex32(elem);
    });
  }

  if (nameBtn) {
    nameBtn.addEventListener('click', () => {
      nameIdx = (nameIdx + 1) % names.length;
      const item = names[nameIdx];
      document.getElementById('code-equ-name').textContent = `\`${item.cn}\``;
      document.getElementById('code-equ-strref').textContent = item.ref;
      document.getElementById('preview-equ-name').textContent = item.cn;
    });
  }

  // Equipment Particle Aura Canvas
  const auraCanvas = document.getElementById('equip-aura-canvas');
  if (auraCanvas) {
    const actx = auraCanvas.getContext('2d');
    const particles = Array.from({ length: 16 }, (_, i) => ({
      angle: (i / 16) * Math.PI * 2,
      radius: 14 + (i % 3) * 6,
      speed: 0.03 + (i % 4) * 0.01,
      size: 1.5 + (i % 2)
    }));

    function drawAura() {
      if (actx && !document.hidden) {
        actx.clearRect(0, 0, 96, 96);
        particles.forEach(p => {
          p.angle += p.speed;
          const x = 48 + Math.cos(p.angle) * (p.radius * 1.4);
          const y = 48 + Math.sin(p.angle) * (p.radius * 1.4);
          actx.beginPath();
          actx.arc(x, y, p.size, 0, Math.PI * 2);
          actx.fillStyle = currentAuraColor;
          actx.shadowBlur = 8;
          actx.shadowColor = currentAuraColor;
          actx.fill();
        });
      }
      requestAnimationFrame(drawAura);
    }
    requestAnimationFrame(drawAura);
  }
}

/* --- Workspace 2: Real HTML5 Canvas 2D ANI Animation & Hitbox Engine --- */
function initAniCanvasEngine() {
  const canvas = document.getElementById('ani-canvas');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');
  if (!ctx) return;

  const playToggle = document.getElementById('ani-play-toggle');
  const prevBtn = document.getElementById('ani-step-prev');
  const nextBtn = document.getElementById('ani-step-next');
  const slider = document.getElementById('ani-frame-slider');
  const readout = document.getElementById('ani-frame-readout');
  const speedBtn = document.getElementById('ani-speed-btn');
  const bboxText = document.getElementById('ani-bbox-text');

  const chkGlow = document.getElementById('ani-chk-glow');
  const chkHitbox = document.getElementById('ani-chk-hitbox');
  const chkAxis = document.getElementById('ani-chk-axis');
  const chkDmg = document.getElementById('ani-chk-dmg');

  let isPlaying = true;
  let currentFrame = 0;
  const totalFrames = 12;
  const speeds = [1.0, 2.0, 0.5];
  let speedIdx = 0;
  let lastFrameTime = performance.now();

  const sparks = Array.from({ length: 24 }, () => ({
    x: 0,
    y: 0,
    vx: (Math.random() - 0.2) * 5,
    vy: (Math.random() - 0.5) * 4,
    alpha: Math.random()
  }));

  function updateReadout() {
    const fStr = String(currentFrame + 1).padStart(2, '0');
    if (readout) readout.textContent = `Frame: ${fStr} / ${totalFrames}`;
    if (slider) slider.value = currentFrame;
    if (bboxText) {
      const reach = 140 + currentFrame * 18;
      bboxText.textContent = `[-90, -75, ${reach}, 48]`;
    }
  }

  if (playToggle) {
    playToggle.addEventListener('click', () => {
      isPlaying = !isPlaying;
      playToggle.textContent = isPlaying ? '⏸ 暂停' : '▶ 播放';
    });
  }

  if (prevBtn) {
    prevBtn.addEventListener('click', () => {
      isPlaying = false;
      if (playToggle) playToggle.textContent = '▶ 播放';
      currentFrame = (currentFrame - 1 + totalFrames) % totalFrames;
      updateReadout();
    });
  }

  if (nextBtn) {
    nextBtn.addEventListener('click', () => {
      isPlaying = false;
      if (playToggle) playToggle.textContent = '▶ 播放';
      currentFrame = (currentFrame + 1) % totalFrames;
      updateReadout();
    });
  }

  if (slider) {
    slider.addEventListener('input', () => {
      isPlaying = false;
      if (playToggle) playToggle.textContent = '▶ 播放';
      currentFrame = parseInt(slider.value, 10) || 0;
      updateReadout();
    });
  }

  if (speedBtn) {
    speedBtn.addEventListener('click', () => {
      speedIdx = (speedIdx + 1) % speeds.length;
      speedBtn.textContent = `${speeds[speedIdx].toFixed(1)}x`;
    });
  }

  function renderAni(now) {
    const aniView = canvas.closest('.mockup-view');
    if (aniView && aniView.classList.contains('active')) {
      const rect = canvas.getBoundingClientRect();
      if (rect.width > 0 && (canvas.width !== rect.width || canvas.height !== rect.height)) {
        canvas.width = rect.width;
        canvas.height = rect.height;
      }

      const interval = 95 / speeds[speedIdx];
      if (isPlaying && now - lastFrameTime >= interval) {
        currentFrame = (currentFrame + 1) % totalFrames;
        lastFrameTime = now;
        updateReadout();
      }

      const w = canvas.width;
      const h = canvas.height;
      const ox = Math.floor(w * 0.36);
      const oy = Math.floor(h * 0.68);

      ctx.clearRect(0, 0, w, h);

      // Background subtle grid
      ctx.strokeStyle = 'rgba(148, 163, 184, 0.07)';
      ctx.lineWidth = 1;
      for (let gx = 0; gx < w; gx += 28) {
        ctx.beginPath();
        ctx.moveTo(gx, 0);
        ctx.lineTo(gx, h);
        ctx.stroke();
      }
      for (let gy = 0; gy < h; gy += 28) {
        ctx.beginPath();
        ctx.moveTo(0, gy);
        ctx.lineTo(w, gy);
        ctx.stroke();
      }

      // Origin X/Y Axis
      if (!chkAxis || chkAxis.checked) {
        ctx.strokeStyle = 'rgba(56, 189, 248, 0.4)';
        ctx.beginPath();
        ctx.moveTo(0, oy);
        ctx.lineTo(w, oy);
        ctx.moveTo(ox, 0);
        ctx.lineTo(ox, h);
        ctx.stroke();
        ctx.fillStyle = '#38bdf8';
        ctx.font = '10px "JetBrains Mono", monospace';
        ctx.fillText('Origin (0, 0)', ox + 6, oy + 14);
      }

      // Ground shadow
      ctx.fillStyle = 'rgba(0, 0, 0, 0.55)';
      ctx.beginPath();
      ctx.ellipse(ox, oy, 38, 10, 0, 0, Math.PI * 2);
      ctx.fill();

      // Character Body Posture based on frame
      const lunge = currentFrame >= 3 && currentFrame <= 8 ? 22 : 0;
      const cx = ox + lunge;
      const cy = oy;

      // Cape flutter
      ctx.fillStyle = '#e11d48';
      ctx.beginPath();
      ctx.moveTo(cx - 8, cy - 52);
      ctx.quadraticCurveTo(cx - 42 - currentFrame * 2, cy - 34, cx - 34, cy - 12);
      ctx.lineTo(cx - 4, cy - 22);
      ctx.closePath();
      ctx.fill();

      // Cyber Swordman Silhouette & Armor
      ctx.fillStyle = '#cbd5e1';
      ctx.fillRect(cx - 10, cy - 56, 20, 34); // Torso
      ctx.fillStyle = '#3b82f6';
      ctx.fillRect(cx - 8, cy - 52, 16, 12); // Chest core
      // Head & Visor
      ctx.fillStyle = '#f8fafc';
      ctx.beginPath();
      ctx.arc(cx, cy - 66, 11, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = '#22d3ee';
      ctx.fillRect(cx + 1, cy - 69, 10, 4); // Cyber visor
      // Legs
      ctx.strokeStyle = '#94a3b8';
      ctx.lineWidth = 6;
      ctx.beginPath();
      ctx.moveTo(cx - 5, cy - 22);
      ctx.lineTo(cx - 14, cy);
      ctx.moveTo(cx + 5, cy - 22);
      ctx.lineTo(cx + 16, cy);
      ctx.stroke();

      // DXT5 Linear Dodge Sword Slash Wave (Frames 2..10)
      if ((!chkGlow || chkGlow.checked) && currentFrame >= 2 && currentFrame <= 10) {
        const progress = (currentFrame - 2) / 8;
        const arcRadius = 75 + progress * 125;

        ctx.save();
        ctx.globalCompositeOperation = 'lighter';
        const grad = ctx.createRadialGradient(cx + 20, cy - 38, 20, cx + 20, cy - 38, arcRadius + 30);
        grad.addColorStop(0, 'rgba(59, 130, 246, 0)');
        grad.addColorStop(0.65, 'rgba(34, 211, 238, 0.55)');
        grad.addColorStop(0.9, 'rgba(168, 85, 247, 0.85)');
        grad.addColorStop(1, 'rgba(255, 255, 255, 0)');

        ctx.strokeStyle = grad;
        ctx.lineWidth = 26 * (1 - progress * 0.45);
        ctx.beginPath();
        ctx.arc(cx + 15, cy - 38, arcRadius, -Math.PI * 0.42, Math.PI * 0.32);
        ctx.stroke();

        // Inner bright blade core
        ctx.strokeStyle = `rgba(224, 242, 254, ${1 - progress * 0.6})`;
        ctx.lineWidth = 4;
        ctx.beginPath();
        ctx.arc(cx + 15, cy - 38, arcRadius + 6, -Math.PI * 0.38, Math.PI * 0.28);
        ctx.stroke();

        // Sparks
        sparks.forEach((sp, idx) => {
          const sx = cx + 45 + ((idx * 17 + currentFrame * 24) % 180);
          const sy = cy - 75 + ((idx * 29) % 85);
          ctx.fillStyle = idx % 2 === 0 ? '#22d3ee' : '#fbbf24';
          ctx.fillRect(sx, sy, 3, 3);
        });
        ctx.restore();
      }

      // Hitbox (Cyan) & Damage Box (Rose) Wireframes
      if (!chkHitbox || chkHitbox.checked) {
        // Character Hitbox [hit box]
        ctx.strokeStyle = 'rgba(34, 211, 238, 0.85)';
        ctx.lineWidth = 1.2;
        ctx.strokeRect(cx - 22, cy - 82, 44, 82);
        ctx.fillStyle = 'rgba(34, 211, 238, 0.9)';
        ctx.font = '10px "JetBrains Mono", monospace';
        ctx.fillText('[hit box]', cx - 22, cy - 86);

        // Attack Box [damage box] active during slash frames
        if (currentFrame >= 2 && currentFrame <= 9) {
          const atkW = 130 + (currentFrame - 2) * 16;
          ctx.strokeStyle = 'rgba(244, 63, 94, 0.95)';
          ctx.fillStyle = 'rgba(244, 63, 94, 0.12)';
          ctx.fillRect(cx + 10, cy - 92, atkW, 88);
          ctx.strokeRect(cx + 10, cy - 92, atkW, 88);
          ctx.fillStyle = '#fb7185';
          ctx.fillText(`[damage box] w:${atkW}`, cx + 14, cy - 97);
        }
      }

      // Floating Critical Damage Text
      if ((!chkDmg || chkDmg.checked) && currentFrame >= 3 && currentFrame <= 10) {
        const floatY = cy - 88 - (currentFrame - 3) * 5;
        ctx.font = 'bold 16px "JetBrains Mono", monospace';
        ctx.fillStyle = '#fbbf24';
        ctx.fillText('⚡ CRITICAL 99,999,999', cx + 55, floatY);
      }
    }
    requestAnimationFrame(renderAni);
  }

  requestAnimationFrame(renderAni);
}

/* --- Workspace 3: Visual GUI Suite (Skill Tree + Level Matrix) --- */
function initGuiSuiteWorkspace() {
  const submodeBtns = document.querySelectorAll('.gui-submode-btn');
  const subSkill = document.getElementById('gui-subview-skill');
  const subMall = document.getElementById('gui-subview-mall');

  submodeBtns.forEach(btn => {
    btn.addEventListener('click', () => {
      submodeBtns.forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      const isSkill = btn.dataset.guimode === 'skill';
      if (subSkill) subSkill.style.display = isSkill ? 'block' : 'none';
      if (subMall) subMall.style.display = isSkill ? 'none' : 'block';
    });
  });

  const sklCards = document.querySelectorAll('.skill-node-card[data-skl]');
  const lvSlider = document.getElementById('skl-lv-slider');
  const lvLabel = document.getElementById('skl-lv-label');
  const colLv = document.getElementById('skl-col-lv');
  const boostBtn = document.getElementById('skl-boost-btn');

  const skillsData = {
    draw: { name: '拔刀斩', basePct: 1840, stepPct: 190, baseInd: 12.5, stepInd: 1.35 },
    illusion: { name: '幻影剑舞', basePct: 3200, stepPct: 310, baseInd: 24.0, stepInd: 2.1 },
    storm: { name: '极·鬼剑术(暴风式)', basePct: 8500, stepPct: 640, baseInd: 58.0, stepInd: 4.5 }
  };

  let activeSkillKey = 'draw';
  let multiplier = 1.0;

  function renderSkillMatrix() {
    const lv = parseInt(lvSlider?.value || '25', 10);
    const s = skillsData[activeSkillKey];
    if (!s) return;

    if (lvLabel) lvLabel.textContent = lv;
    if (colLv) colLv.textContent = `Lv.${lv}`;

    const curPct = Math.round((s.basePct + (lv - 1) * s.stepPct) * multiplier);
    const maxPct = Math.round((s.basePct + 39 * s.stepPct) * multiplier);
    const curInd = ((s.baseInd + (lv - 1) * s.stepInd) * multiplier).toFixed(1);
    const maxInd = ((s.baseInd + 39 * s.stepInd) * multiplier).toFixed(1);
    const rangePct = Math.min(200, 100 + Math.round(lv * 1.4));

    const c1Cur = document.getElementById('skl-c1-cur');
    const c1Max = document.getElementById('skl-c1-max');
    const c2Cur = document.getElementById('skl-c2-cur');
    const c2Max = document.getElementById('skl-c2-max');
    const c3Cur = document.getElementById('skl-c3-cur');

    if (c1Cur) c1Cur.textContent = `${curPct.toLocaleString()}%`;
    if (c1Max) c1Max.textContent = `${maxPct.toLocaleString()}%`;
    if (c2Cur) c2Cur.textContent = `${curInd}x`;
    if (c2Max) c2Max.textContent = `${maxInd}x`;
    if (c3Cur) c3Cur.textContent = `${rangePct}%`;

    const pName = document.getElementById('skl-preview-name');
    const pDesc = document.getElementById('skl-preview-desc');
    if (pName) pName.textContent = `${s.name} (Lv.${lv}${multiplier > 1 ? ' · 已倍增' : ''})`;
    if (pDesc) {
      pDesc.innerHTML = `
        向周围敌人发出极限威力的剑术斩击。<br>
        多段斩击物理攻击力：<span style="color:#fbbf24;font-weight:700;">${curPct.toLocaleString()}%</span><br>
        终结剑气独立攻击力：<span style="color:#34d399;font-weight:700;">${curInd}倍</span><br>
        斩击索敌范围比率：<span style="color:#38bdf8;font-weight:700;">${rangePct}%</span>
      `;
    }
  }

  sklCards.forEach(card => {
    card.addEventListener('click', () => {
      sklCards.forEach(c => c.classList.remove('active'));
      card.classList.add('active');
      activeSkillKey = card.dataset.skl;
      renderSkillMatrix();
    });
  });

  if (lvSlider) {
    lvSlider.addEventListener('input', renderSkillMatrix);
  }

  if (boostBtn) {
    boostBtn.addEventListener('click', () => {
      multiplier = multiplier > 1.5 ? 1.0 : +(multiplier * 1.2).toFixed(2);
      boostBtn.textContent = multiplier === 1.0 ? '全等级倍率 ×1.2' : `当前倍率 ×${multiplier} (点击叠加)`;
      renderSkillMatrix();
    });
  }
}

/* --- Workspace 4: CompareWorkbench (Dual-Pane Archive Diff & Merge) --- */
function initCompareWorkbenchDemo() {
  const singleBtns = document.querySelectorAll('.diff-merge-single');
  const mergeAllBtn = document.getElementById('diff-merge-all-btn');
  const resetBtn = document.getElementById('diff-reset-btn');
  const statusCounter = document.getElementById('diff-status-counter');
  const statusDirty = document.getElementById('status-dirty-indicator');

  const mergedState = { 1: false, 2: false };

  function mergeHunk(id) {
    mergedState[id] = true;
    const target = document.getElementById(`diff-target-hunk-${id}`);
    if (target) {
      target.className = 'diff-hunk diff-hunk-merged';
      if (id === 1 || id === '1') {
        target.innerHTML = '✓ [已合入] [physical attack] 1680 / [magical attack] 1920';
      } else {
        target.innerHTML = '✓ [已合入] [attack damage rate] 40 / [all elemental attack] 30';
      }
    }
    const remaining = Object.values(mergedState).filter(v => !v).length;
    if (statusCounter) {
      statusCounter.textContent = remaining === 0 ? '✓ 全部差异已合入 (可按 Cmd+S 保存)' : `待合并差异: ${remaining} 处`;
    }
    if (statusDirty) {
      statusDirty.textContent = `Dirty Chunks: ${3 - remaining} / 65,536`;
    }
  }

  singleBtns.forEach(btn => {
    btn.addEventListener('click', () => mergeHunk(btn.dataset.hunk));
  });

  if (mergeAllBtn) {
    mergeAllBtn.addEventListener('click', () => {
      mergeHunk(1);
      mergeHunk(2);
    });
  }

  if (resetBtn) {
    resetBtn.addEventListener('click', () => {
      mergedState[1] = false;
      mergedState[2] = false;
      const t1 = document.getElementById('diff-target-hunk-1');
      const t2 = document.getElementById('diff-target-hunk-2');
      if (t1) {
        t1.className = 'diff-hunk diff-hunk-del';
        t1.innerHTML = '- [physical attack] 1120<br>- [magical attack] 1350';
      }
      if (t2) {
        t2.className = 'diff-hunk diff-hunk-del';
        t2.innerHTML = '- [attack damage rate] 25';
      }
      if (statusCounter) statusCounter.textContent = '待合并差异: 2 处';
    });
  }
}

/* --- Workspace 5: MCP AI Server (:19245) Playground --- */
function initMcpPlaygroundDemo() {
  const btns = document.querySelectorAll('.mcp-prompt-btn');
  const term = document.getElementById('mcp-terminal-output');
  if (!btns.length || !term) return;

  const responses = {
    explain: `
      <div style="color: #a78bfa;">// Streamable HTTP MCP Server listening on http://127.0.0.1:19245/mcp</div>
      <div class="mcp-log-entry">
        <div style="color: #38bdf8;">&gt; Tool Call: <code>pvf_annotation_explain</code></div>
        <div style="color: #94a3b8;">Params: <code>{"path": "equipment/character/common/amulet/100300001.equ"}</code></div>
      </div>
      <div class="mcp-log-entry">
        <div style="color: #34d399;">&lt; Response (9ms):</div>
        <div>• <code>[grade] = 5</code> &rarr; 标注枚举映射: <strong>史诗 (Epic)</strong></div>
        <div>• <code>[name] = {8=&lt;31::amulet_star_01&gt;}</code> &rarr; 115CN 宽字符表译文: <strong>王者之星·神圣护符</strong></div>
        <div>• <code>equipment.lst</code> 反向索引: ID <code>100300001</code> 路径一致</div>
      </div>
    `,
    drop: `
      <div style="color: #a78bfa;">// Active Skill: .agents/skills/pvf-drop-rate/SKILL.md</div>
      <div class="mcp-log-entry">
        <div style="color: #38bdf8;">&gt; Tool Call: <code>pvf_script_preview</code></div>
        <div style="color: #94a3b8;">Params: <code>{"target": "etc/itemdropinfo_common.etc", "multiplier": 3.0}</code></div>
      </div>
      <div class="mcp-log-entry">
        <div style="color: #34d399;">&lt; Preview Generated (14ms):</div>
        <div>• 已生成工作区草稿预览（未直接覆盖原文件）</div>
        <div>• 深渊派对史诗基准权重: <code>120 &rarr; 360 (+200%)</code>，支持一键应用至工作区</div>
      </div>
    `,
    mod: `
      <div style="color: #a78bfa;">// MOD Packaging &amp; Dependency Resolver Engine</div>
      <div class="mcp-log-entry">
        <div style="color: #38bdf8;">&gt; Tool Call: <code>pvf_mod_export</code></div>
        <div style="color: #94a3b8;">Params: <code>{"format": "115CN-mod", "mergeWith": "abyss-epic-1.0.4.zip", "modId": "abyss_epic_boost"}</code></div>
      </div>
      <div class="mcp-log-entry">
        <div style="color: #34d399;">&lt; Export Completed (38ms):</div>
        <div>• 自动扫描并补齐关联 <code>equipment.lst</code> 与 <code>.ptl</code> 粒子依赖共 14 项</div>
        <div>• 已合入旧版 MOD 并自动递增版本号 &rarr; <strong><code>abyss-epic-1.0.5.zip</code></strong></div>
      </div>
    `
  };

  btns.forEach(btn => {
    btn.addEventListener('click', () => {
      btns.forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      const key = btn.dataset.mcp;
      if (responses[key]) {
        term.innerHTML = responses[key];
      }
    });
  });
}

/* --- Workspace 6: Goja JS Sandbox Demo --- */
function initGojaSandboxDemo() {
  const runBtn = document.getElementById('run-goja-btn');
  const statusBox = document.getElementById('goja-status-box');
  const diffLines = document.getElementById('goja-diff-lines');
  if (!runBtn || !statusBox) return;

  runBtn.addEventListener('click', () => {
    runBtn.textContent = '⚡ Goja 引擎执行中...';
    statusBox.textContent = '正在扫描并解析索引块...';
    setTimeout(() => {
      runBtn.textContent = '✓ 重新运行沙箱脚本';
      statusBox.innerHTML = '⚡ <strong>执行完成 (耗时 11.4ms)</strong>：已匹配并暂存 <strong>1,428</strong> 个百级史诗文件的三攻修改！';
      if (diffLines) {
        diffLines.innerHTML = `
          <div style="color: #94a3b8; margin-bottom: 0.25rem;">// Staging Diff 抽样 (1,428 files staged)</div>
          <div style="color: #f43f5e;">- [physical attack] 1420  (100300001.equ)</div>
          <div style="color: #34d399;">+ [physical attack] 1633  // +15%</div>
          <div style="color: #f43f5e;">- [magical attack] 1580   (100300002.equ)</div>
          <div style="color: #34d399;">+ [magical attack] 1817   // +15%</div>
        `;
      }
    }, 320);
  });
}

/* ==========================================================================
   9. Interactive 5-Byte Binary Token Decompiler Lab
   ========================================================================== */
function initTokenDecompilerLab() {
  const input = document.getElementById('token-live-input');
  const presets = document.querySelectorAll('.token-preset-btn');
  const container = document.getElementById('token-packets-container');
  const byteCountEl = document.getElementById('token-byte-count');
  const hashOut = document.getElementById('token-hash-out');
  const routeOut = document.getElementById('token-route-out');

  if (!input || !container) return;

  function djb2Hash(str) {
    let hash = 5381;
    for (let i = 0; i < str.length; i++) {
      hash = ((hash << 5) + hash + str.charCodeAt(i)) >>> 0;
    }
    return hash;
  }

  function int32ToLeBytes(n) {
    const val = n | 0;
    return [
      val & 0xff,
      (val >> 8) & 0xff,
      (val >> 16) & 0xff,
      (val >> 24) & 0xff
    ].map(b => b.toString(16).padStart(2, '0').toUpperCase()).join(' ');
  }

  function float32ToLeBytes(f) {
    const buf = new ArrayBuffer(4);
    const view = new DataView(buf);
    view.setFloat32(0, f, true);
    return [0, 1, 2, 3].map(i => view.getUint8(i).toString(16).padStart(2, '0').toUpperCase()).join(' ');
  }

  function compileTokens(rawText) {
    const text = rawText.trim() || '[grade] 5';
    const tokens = text.match(/\[[^\]]+\]|`[^`]+`|\{8=<[^>]+>\}|[-+]?\d*\.\d+|[-+]?\d+|\S+/g) || [text];

    const packets = [
      {
        type: 'Header (2B)',
        opcode: 'MAGIC',
        bytes: 'FF B0',
        meaning: 'PVF Binary Script Header'
      }
    ];

    let hasNonAscii = false;
    let hasStrTableRef = false;

    tokens.forEach(tok => {
      if (tok.startsWith('[') && tok.endsWith(']')) {
        const idx = djb2Hash(tok) & 0xffff;
        packets.push({
          type: '0x05 Section (5B)',
          opcode: 'OP_SECTION',
          bytes: `05 ${int32ToLeBytes(idx)}`,
          meaning: `Section ${tok} (Pool #${idx})`
        });
      } else if (tok.startsWith('{8=<') && tok.endsWith('>}')) {
        hasStrTableRef = true;
        const idx = djb2Hash(tok) & 0xffff;
        packets.push({
          type: '0x08 StrRef (5B)',
          opcode: 'OP_STRREF',
          bytes: `08 1F 00 ${(idx & 0xff).toString(16).padStart(2, '0').toUpperCase()} ${((idx >> 8) & 0xff).toString(16).padStart(2, '0').toUpperCase()}`,
          meaning: `110US/115CN Ref ${tok}`
        });
      } else if (tok.startsWith('`') && tok.endsWith('`')) {
        const inner = tok.slice(1, -1);
        if (/[^\x00-\x7F]/.test(inner)) hasNonAscii = true;
        const idx = djb2Hash(inner) & 0xffff;
        packets.push({
          type: '0x07 String (5B)',
          opcode: 'OP_STRING',
          bytes: `07 ${int32ToLeBytes(idx)}`,
          meaning: `${hasNonAscii ? 'sTrW UTF-16LE' : 'ASCII Pool'}: ${tok}`
        });
      } else if (/^[-+]?\d*\.\d+$/.test(tok)) {
        const f = parseFloat(tok);
        packets.push({
          type: '0x04 Float32 (5B)',
          opcode: 'OP_FLOAT',
          bytes: `04 ${float32ToLeBytes(f)}`,
          meaning: `IEEE-754 Float: ${f}`
        });
      } else if (/^[-+]?\d+$/.test(tok)) {
        const n = parseInt(tok, 10);
        packets.push({
          type: '0x02 Int32 (5B)',
          opcode: 'OP_INT32',
          bytes: `02 ${int32ToLeBytes(n)}`,
          meaning: `Signed Int32: ${n}`
        });
      } else {
        if (/[^\x00-\x7F]/.test(tok)) hasNonAscii = true;
        const idx = djb2Hash(tok) & 0xffff;
        packets.push({
          type: '0x07 Literal (5B)',
          opcode: 'OP_STRING',
          bytes: `07 ${int32ToLeBytes(idx)}`,
          meaning: `Token: ${tok}`
        });
      }
    });

    const totalBytes = 2 + (packets.length - 1) * 5;
    if (byteCountEl) byteCountEl.textContent = `${totalBytes} Bytes (${packets.length - 1} × 5B Tokens)`;

    const h = djb2Hash(text);
    if (hashOut) hashOut.textContent = `0x${h.toString(16).toUpperCase().padStart(8, '0')} (${h})`;
    if (routeOut) {
      if (hasStrTableRef) {
        routeOut.textContent = '110US / 115CN StringTable Placeholder (UTF-16LE In-Place)';
      } else if (hasNonAscii) {
        routeOut.textContent = 'sTrW (UTF-16LE Wide-Char Pool — 自动防乱码路由)';
      } else {
        routeOut.textContent = 'StringTable Main Pool (ASCII Fast Index)';
      }
    }

    container.innerHTML = packets.map(p => `
      <div class="token-packet">
        <div class="packet-type"><span>${p.type}</span><span>${p.opcode}</span></div>
        <div class="packet-bytes">${p.bytes}</div>
        <div class="packet-meaning">${p.meaning}</div>
      </div>
    `).join('');
  }

  input.addEventListener('input', () => compileTokens(input.value));

  presets.forEach(btn => {
    btn.addEventListener('click', () => {
      presets.forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      input.value = btn.dataset.expr;
      compileTokens(input.value);
    });
  });

  compileTokens(input.value);
}

/* ==========================================================================
   10. Benchmark Race Simulator
   ========================================================================== */
function initBenchmarkRunner() {
  const runBtn = document.getElementById('run-benchmark-btn');
  const benchResult = document.getElementById('bench-result-time');
  const barPvfine = document.getElementById('bench-bar-pvfine');
  const barLegacy = document.getElementById('bench-bar-legacy');

  if (!runBtn) return;

  runBtn.addEventListener('click', () => {
    runBtn.disabled = true;
    runBtn.textContent = '⚡ 正在流式解密与构建百万索引...';
    if (benchResult) benchResult.textContent = '正在执行并发分块压测...';
    if (barPvfine) {
      barPvfine.style.transition = 'none';
      barPvfine.style.width = '2%';
    }
    if (barLegacy) {
      barLegacy.style.transition = 'none';
      barLegacy.style.width = '1%';
    }

    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        if (barPvfine) {
          barPvfine.style.transition = 'width 0.74s cubic-bezier(0.16, 1, 0.3, 1)';
          barPvfine.style.width = '96%';
        }
        if (barLegacy) {
          barLegacy.style.transition = 'width 1.8s linear';
          barLegacy.style.width = '14%';
        }
      });
    });

    setTimeout(() => {
      const jitter = (0.71 + Math.random() * 0.06).toFixed(2);
      if (benchResult) {
        benchResult.textContent = `耗时: ${jitter} 秒 • 内存: 48 MB • 1,048,576 文件就绪`;
      }
      runBtn.disabled = false;
      runBtn.textContent = '🔄 重新运行实测对比';
    }, 760);
  });
}

/* ==========================================================================
   11. Developer SDK Tabs & Code Copy
   ========================================================================== */
function initSdkTabsAndCopy() {
  const tabBtns = document.querySelectorAll('.sdk-tab-btn');
  const copyBtn = document.getElementById('sdk-copy-btn');
  const preBlocks = {
    go: document.getElementById('sdk-code-go'),
    mcp: document.getElementById('sdk-code-mcp'),
    js: document.getElementById('sdk-code-js')
  };

  tabBtns.forEach(btn => {
    btn.addEventListener('click', () => {
      tabBtns.forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      const key = btn.dataset.sdktab;
      Object.entries(preBlocks).forEach(([k, el]) => {
        if (el) el.classList.toggle('active', k === key);
      });
      if (copyBtn) copyBtn.dataset.target = `#sdk-code-${key}`;
    });
  });

  if (copyBtn) {
    copyBtn.addEventListener('click', async () => {
      const targetEl = document.querySelector(copyBtn.dataset.target || '#sdk-code-go');
      if (!targetEl) return;
      try {
        await navigator.clipboard.writeText(targetEl.textContent);
        const orig = copyBtn.textContent;
        copyBtn.textContent = '✓ 已复制到剪贴板';
        copyBtn.style.color = '#34d399';
        copyBtn.style.borderColor = '#34d399';
        setTimeout(() => {
          copyBtn.textContent = orig;
          copyBtn.style.color = '';
          copyBtn.style.borderColor = '';
        }, 1800);
      } catch (err) {
        console.error('Copy failed:', err);
      }
    });
  }
}

/* ==========================================================================
   12. Command Palette Modal (⌘K / Ctrl+K) & Release Notes Modal
   ========================================================================== */
function initCommandPalette() {
  const dialog = document.getElementById('cmd-palette-dialog');
  const openBtn = document.getElementById('open-cmd-btn');
  const closeBtn = document.getElementById('close-cmd-btn');
  const searchInput = document.getElementById('cmd-search-input');
  const cmdItems = document.querySelectorAll('.cmd-item');

  if (!dialog) return;

  function openPalette() {
    if (!dialog.open) {
      dialog.showModal();
      if (searchInput) {
        searchInput.value = '';
        filterCommands('');
        searchInput.focus();
      }
    }
  }

  function closePalette() {
    if (dialog.open) dialog.close();
  }

  if (openBtn) openBtn.addEventListener('click', openPalette);
  if (closeBtn) closeBtn.addEventListener('click', closePalette);

  dialog.addEventListener('click', e => {
    if (e.target === dialog) closePalette();
  });

  window.addEventListener('keydown', e => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      if (dialog.open) closePalette();
      else openPalette();
    }
  });

  function filterCommands(q) {
    const query = q.trim().toLowerCase();
    cmdItems.forEach(item => {
      const match = !query || item.textContent.toLowerCase().includes(query);
      item.style.display = match ? 'flex' : 'none';
    });
  }

  if (searchInput) {
    searchInput.addEventListener('input', () => filterCommands(searchInput.value));
  }

  cmdItems.forEach(item => {
    item.addEventListener('click', () => {
      const cmd = item.dataset.cmd;
      closePalette();

      if (cmd === 'download-os') {
        const heroBtn = document.getElementById('hero-download-btn');
        if (heroBtn) heroBtn.click();
      } else if (cmd && cmd.startsWith('view-')) {
        const viewName = cmd.replace('view-', '');
        document.getElementById('showcase')?.scrollIntoView({ behavior: 'smooth' });
        if (switchIdeViewGlobal) switchIdeViewGlobal(viewName);
      } else if (cmd === 'sec-pipeline') {
        document.getElementById('pipeline')?.scrollIntoView({ behavior: 'smooth' });
      } else if (cmd === 'sec-comparison') {
        document.getElementById('comparison')?.scrollIntoView({ behavior: 'smooth' });
      } else if (cmd === 'toggle-theme') {
        document.getElementById('theme-toggle-btn')?.click();
      }
    });
  });
}

function initReleaseNotesModal() {
  const dialog = document.getElementById('release-notes-dialog');
  const openBtn = document.getElementById('open-release-notes-btn');
  const closeBtn = document.getElementById('close-release-notes-btn');
  const notesBody = document.getElementById('release-notes-body');

  if (!dialog) return;

  // Provide rich v1.0.0 fallback release notes if GitHub API hasn't populated yet
  if (notesBody && notesBody.textContent.includes('正在从 GitHub')) {
    notesBody.textContent = [
      '✨ pvfine v1.0.0 正式版里程碑核心亮点：',
      '• 发布首个稳定正式版，汇集多版本 PVF 无损读写、结构化 GUI 编辑、归档对比合并、本地版本控制、MOD 导入导出及 MCP 服务等核心能力。',
      '• 新增商城、抽奖机和娃娃机图形界面，支持可视化查看和编辑相关数据。',
      '• MOD 导出支持合并同格式的已有 MOD，并自动递增版本号。',
      '• 内置 115CN 分页归档解析支持，适配对应标注、渲染、GUI 编辑及 115CN-mod 压缩包导入导出。',
      '• 内置本机 MCP 服务 (:19245) 与 5 类领域 AI Skill（掉率/技能数值/强化锻造/商店/任务）。',
      '• 内嵌双栏归档差异对比与实时合并工作台 (CompareWorkbench)。'
    ].join('\n');
  }

  if (openBtn) {
    openBtn.addEventListener('click', () => {
      if (!dialog.open) dialog.showModal();
    });
  }
  if (closeBtn) {
    closeBtn.addEventListener('click', () => {
      if (dialog.open) dialog.close();
    });
  }
  dialog.addEventListener('click', e => {
    if (e.target === dialog) dialog.close();
  });
}
