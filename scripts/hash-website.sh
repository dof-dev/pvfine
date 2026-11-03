#!/usr/bin/env bash
set -euo pipefail

# 自动计算 website 静态资源 (CSS / JS / 图标) 的 SHA-256 内容哈希，
# 重命名文件为 style.<hash>.css / main.<hash>.js 并同步更新 website/index.html，
# 彻底穿透浏览器缓存与各类 CDN（含开启“忽略 URL 参数”的 CDN 节点）。

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="$ROOT_DIR/website"

cd "$WEB_DIR"

CSS_FILE="$(find . -maxdepth 1 -type f -name 'style*.css' | head -n 1)"
JS_FILE="$(find . -maxdepth 1 -type f -name 'main*.js' | head -n 1)"

if [[ -z "$CSS_FILE" || -z "$JS_FILE" ]]; then
  echo "Error: style*.css or main*.js not found in $WEB_DIR" >&2
  exit 1
fi

CSS_HASH="$(shasum -a 256 "$CSS_FILE" | cut -c1-8)"
JS_HASH="$(shasum -a 256 "$JS_FILE" | cut -c1-8)"
ICON_HASH="$(shasum -a 256 "assets/appicon.png" | cut -c1-8)"

NEW_CSS="style.${CSS_HASH}.css"
NEW_JS="main.${JS_HASH}.js"

if [[ "$(basename "$CSS_FILE")" != "$NEW_CSS" ]]; then
  mv "$CSS_FILE" "$NEW_CSS"
fi

if [[ "$(basename "$JS_FILE")" != "$NEW_JS" ]]; then
  mv "$JS_FILE" "$NEW_JS"
fi

python3 - "$NEW_CSS" "$NEW_JS" "$ICON_HASH" <<'PY'
import pathlib, re, sys

new_css, new_js, icon_hash = sys.argv[1], sys.argv[2], sys.argv[3]
index_path = pathlib.Path("index.html")
html = index_path.read_text(encoding="utf-8")

html = re.sub(r'href="style(?:\.[a-f0-9]+)?\.css(?:\?v=[^"]*)?"', f'href="{new_css}"', html)
html = re.sub(r'src="main(?:\.[a-f0-9]+)?\.js(?:\?v=[^"]*)?"', f'src="{new_js}"', html)
html = re.sub(r'assets/appicon\.png(?:\?v=[^"]*)?', f'assets/appicon.png?v={icon_hash}', html)

index_path.write_text(html, encoding="utf-8")
print(f"Updated index.html -> {new_css}, {new_js}, appicon.png?v={icon_hash}")
PY
