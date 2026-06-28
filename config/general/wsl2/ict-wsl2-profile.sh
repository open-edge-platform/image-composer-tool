# ICT WSL2 profile defaults. This file is sourced by interactive login shells.

case "$-" in
  *i*) ;;
  *) return 0 2>/dev/null || exit 0 ;;
esac

if ! grep -qiE '(microsoft|wsl)' /proc/sys/kernel/osrelease 2>/dev/null; then
  return 0 2>/dev/null || exit 0
fi

if [ "${ICT_WSL2_PRESERVE_CWD:-}" != "1" ] && [ -n "${HOME:-}" ] && [ -d "$HOME" ]; then
  case "$PWD" in
    "$HOME"|"$HOME"/*) ;;
    *) cd "$HOME" 2>/dev/null || true ;;
  esac
fi

instructions="/usr/share/doc/ict-wsl2/resize-filesystem.txt"
marker="${HOME:-/tmp}/.ict-wsl2-welcome-shown"
if [ ! -e "$marker" ]; then
  if [ -r "$instructions" ]; then
    awk -v distro="${WSL_DISTRO_NAME:-<distribution-name>}" \
      '{gsub(/<distribution-name>/, distro); print}' "$instructions"
  fi
  touch "$marker" 2>/dev/null || true
fi
