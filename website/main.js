/**
 * pvfine Official Website - Interactive Scripts & GitHub Release Loader
 */

document.addEventListener('DOMContentLoaded', () => {
  initAppSimulator();
  const detectedOs = initOsDetection();
  loadGitHubReleaseInfo(detectedOs);
  initCodeCopy();
  initScrollNav();
  initThemeToggle();
  initBenchmarkRunner();
});

/* ==========================================================================
   1. Interactive App Simulator (Hero Mockup)
   ========================================================================== */
function initAppSimulator() {
  const treeItems = document.querySelectorAll('.tree-item[data-view]');
  const appTabs = document.querySelectorAll('.app-tab[data-view]');
  const mockupViews = document.querySelectorAll('.mockup-view');
  const windowFileName = document.getElementById('window-active-filename');

  const fileTitles = {
    'equ': 'equipment/character/common/amulet/100300001.equ',
    'ani': 'character/swordman/effect/animation/skill_slash.ani',
    'shop': 'etc/shop/seria_shop.shp [GUI Mode]',
    'script': 'workspace/scripts/batch_rarity_boost.pvf.js'
  };

  function switchView(viewName) {
    // 1. Update tree selection
    treeItems.forEach(item => {
      item.classList.toggle('active', item.dataset.view === viewName);
    });

    // 2. Update tabs
    appTabs.forEach(tab => {
      tab.classList.toggle('active', tab.dataset.view === viewName);
    });

    // 3. Update view containers
    mockupViews.forEach(view => {
      const isTarget = view.classList.contains(`view-${viewName}`);
      view.classList.toggle('active', isTarget);
    });

    // 4. Update title in titlebar
    if (windowFileName && fileTitles[viewName]) {
      windowFileName.textContent = fileTitles[viewName];
    }
  }

  treeItems.forEach(item => {
    item.addEventListener('click', () => {
      switchView(item.dataset.view);
    });
  });

  appTabs.forEach(tab => {
    tab.addEventListener('click', () => {
      switchView(tab.dataset.view);
    });
  });

  // ANI Play/Pause toggle
  const aniPlayBtn = document.getElementById('ani-play-toggle');
  const aniCharacter = document.querySelector('.ani-character');
  if (aniPlayBtn && aniCharacter) {
    let isPlaying = true;
    aniPlayBtn.addEventListener('click', () => {
      isPlaying = !isPlaying;
      if (isPlaying) {
        aniCharacter.style.animationPlayState = 'running';
        aniPlayBtn.textContent = '⏸ 暂停';
      } else {
        aniCharacter.style.animationPlayState = 'paused';
        aniPlayBtn.textContent = '▶ 播放';
      }
    });
  }
}

/* ==========================================================================
   2. OS Detection
   ========================================================================== */
function initOsDetection() {
  const ua = navigator.userAgent.toLowerCase();
  let detectedOs = 'windows';

  if (ua.includes('mac') || ua.includes('darwin')) {
    detectedOs = 'macos';
  } else if (ua.includes('linux')) {
    detectedOs = 'linux';
  }

  // Highlight corresponding download card
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
   3. GitHub Releases API - Dynamic Version & Asset Direct Links
   ========================================================================== */
async function loadGitHubReleaseInfo(detectedOs) {
  const repo = 'dof-dev/pvfine';
  const apiUrl = `https://api.github.com/repos/${repo}/releases/latest`;
  const defaultReleaseUrl = `https://github.com/${repo}/releases/latest`;

  try {
    const response = await fetch(apiUrl);
    if (!response.ok) {
      console.warn(`GitHub API request returned ${response.status}, using default release page link.`);
      return;
    }
    const data = await response.json();
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
  const tag = data.tag_name || 'v0.7.0';
  const releaseUrl = data.html_url || defaultReleaseUrl;
  const assets = data.assets || [];

  // 1. Update version badge in Header
  const headerVersionPill = document.getElementById('release-version-pill');
  if (headerVersionPill) {
    headerVersionPill.textContent = tag;
  }

  // 2. Match assets for each platform
  const macUniversal = assets.find(a => 
    a.name.endsWith('.dmg') && (a.name.includes('universal') || a.name.includes('macos') || a.name.includes('darwin'))
  );
  const macIntel = assets.find(a => 
    a.name.endsWith('.dmg') && a.name.includes('amd64')
  ) || macUniversal;

  const winInstaller = assets.find(a => 
    a.name.endsWith('.exe') && (a.name.includes('installer') || a.name.includes('setup'))
  );
  const winPortable = assets.find(a => 
    (a.name.endsWith('.exe') || a.name.endsWith('.zip')) && a.name.includes('windows') && !a.name.includes('installer') && !a.name.includes('setup')
  );

  const linuxAppImage = assets.find(a => a.name.endsWith('.AppImage'));
  const linuxDeb = assets.find(a => a.name.endsWith('.deb'));

  // 3. Update Download Matrix Cards buttons
  bindAssetButton('[data-asset-type="mac-universal"]', macUniversal, 'macOS 通用安装包 (.dmg)', releaseUrl);
  bindAssetButton('[data-asset-type="mac-intel"]', macIntel, 'Intel x86_64 专用 (.dmg)', releaseUrl);
  bindAssetButton('[data-asset-type="win-installer"]', winInstaller, 'Windows 自动安装包 (.exe)', releaseUrl);
  bindAssetButton('[data-asset-type="win-portable"]', winPortable, '便携独立运行版 (.exe)', releaseUrl);
  bindAssetButton('[data-asset-type="linux-appimage"]', linuxAppImage, 'Universal AppImage', releaseUrl);
  bindAssetButton('[data-asset-type="linux-deb"]', linuxDeb, 'Debian / Ubuntu (.deb)', releaseUrl);

  // 4. Update Hero CTA Button to direct download link based on detected OS
  const heroDownloadBtn = document.getElementById('hero-download-btn');
  const heroDownloadDesc = document.getElementById('hero-download-hint');

  if (heroDownloadBtn) {
    let targetAsset = null;
    let osLabel = '最新版本';

    if (detectedOs === 'macos') {
      targetAsset = macUniversal;
      osLabel = 'macOS 通用版';
    } else if (detectedOs === 'windows') {
      targetAsset = winInstaller || winPortable;
      osLabel = 'Windows 64-bit';
    } else if (detectedOs === 'linux') {
      targetAsset = linuxAppImage || linuxDeb;
      osLabel = 'Linux';
    }

    if (targetAsset && targetAsset.browser_download_url) {
      heroDownloadBtn.href = targetAsset.browser_download_url;
      const sizeStr = formatFileSize(targetAsset.size);
      heroDownloadBtn.textContent = `下载 ${osLabel} (${tag}${sizeStr ? ' · ' + sizeStr : ''})`;
      heroDownloadBtn.setAttribute('download', '');
    } else {
      heroDownloadBtn.href = releaseUrl;
      heroDownloadBtn.textContent = `下载 ${osLabel} (${tag})`;
    }
  }

  if (heroDownloadDesc) {
    heroDownloadDesc.textContent = `当前最新版本 ${tag} · 自动直链 GitHub Release 附件 · GPL-3.0 开源`;
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
    btn.title = `前往 GitHub Release 页面下载`;
  }
}

/* ==========================================================================
   4. Code Copy
   ========================================================================== */
function initCodeCopy() {
  const copyButtons = document.querySelectorAll('.copy-btn');

  copyButtons.forEach(btn => {
    btn.addEventListener('click', async () => {
      const targetSelector = btn.dataset.target;
      const codeElement = document.querySelector(targetSelector);
      if (!codeElement) return;

      const codeText = codeElement.textContent;
      try {
        await navigator.clipboard.writeText(codeText);
        const originalHtml = btn.innerHTML;
        btn.innerHTML = `✓ 已复制`;
        btn.style.color = '#34d399';
        btn.style.borderColor = '#34d399';

        setTimeout(() => {
          btn.innerHTML = originalHtml;
          btn.style.color = '';
          btn.style.borderColor = '';
        }, 2000);
      } catch (err) {
        console.error('Copy failed', err);
      }
    });
  });
}

/* ==========================================================================
   5. Scroll Header Glass Effect
   ========================================================================== */
function initScrollNav() {
  const header = document.querySelector('.site-header');
  window.addEventListener('scroll', () => {
    if (window.scrollY > 20) {
      header.classList.add('scrolled');
    } else {
      header.classList.remove('scrolled');
    }
  });
}

/* ==========================================================================
   6. Theme Toggle (Dark / Light)
   ========================================================================== */
function initThemeToggle() {
  const toggleBtn = document.getElementById('theme-toggle-btn');
  if (!toggleBtn) return;

  const savedTheme = localStorage.getItem('pvfine-theme');
  const systemPrefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
  const currentTheme = savedTheme || (systemPrefersDark ? 'dark' : 'light');

  applyThemeToPage(currentTheme);
  updateToggleIcon(toggleBtn, currentTheme);

  toggleBtn.addEventListener('click', () => {
    const active = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
    applyThemeToPage(active);
    localStorage.setItem('pvfine-theme', active);
    updateToggleIcon(toggleBtn, active);
  });
}

function applyThemeToPage(theme) {
  document.documentElement.setAttribute('data-theme', theme);
  document.documentElement.style.colorScheme = theme;
}

function updateToggleIcon(btn, theme) {
  btn.textContent = theme === 'dark' ? '🌙' : '☀️';
}

/* ==========================================================================
   7. Benchmark Interactive Simulator
   ========================================================================== */
function initBenchmarkRunner() {
  const runBtn = document.getElementById('run-benchmark-btn');
  const benchResult = document.getElementById('bench-result-time');
  const benchFiles = document.getElementById('bench-result-files');
  const benchMem = document.getElementById('bench-result-mem');

  if (!runBtn) return;

  runBtn.addEventListener('click', () => {
    runBtn.disabled = true;
    runBtn.textContent = '⚡️ 正在极速解密与建构索引...';
    if (benchResult) benchResult.textContent = '计算中...';

    setTimeout(() => {
      if (benchResult) benchResult.textContent = '0.78 秒';
      if (benchFiles) benchFiles.textContent = '1,048,576 文件';
      if (benchMem) benchMem.textContent = '48 MB (按需分块)';
      runBtn.disabled = false;
      runBtn.textContent = '🔄 重新运行基准测试';
    }, 850);
  });
}
